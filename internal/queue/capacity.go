package queue

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

// ErrCapacity means the caller must retry later; no message was accepted.
var ErrCapacity = errors.New("queue capacity temporarily unavailable")

// CapacityLimits apply to pending messages in one exclusively owned local queue.
// MaxBytes counts message Data bytes, including headers, rather than JSON overhead.
// Zero disables each limit. LowPercent defaults to 80 when unset.
type CapacityLimits struct {
	MaxMessages, MaxBytes, MinFreeBytes, LowPercent int64
}

func (l CapacityLimits) enabled() bool {
	return l.MaxMessages != 0 || l.MaxBytes != 0 || l.MinFreeBytes != 0
}
func (l CapacityLimits) validate() error {
	if l.MaxMessages < 0 || l.MaxBytes < 0 || l.MinFreeBytes < 0 || l.LowPercent < 0 || l.LowPercent >= 100 {
		return errors.New("queue limits must be nonnegative and low watermark must be below 100 percent")
	}
	return nil
}

// SetMetrics attaches capacity monitoring to a backend created by NewBackend.
func SetMetrics(b Backend, m *observability.Metrics) {
	if observed, ok := b.(*observedBackend); ok {
		SetMetrics(observed.next, m)
	}
	if s, ok := b.(*Store); ok {
		s.mu.Lock()
		s.metrics = m
		s.refreshCapacityMetrics()
		s.mu.Unlock()
	}
}

func (s *Store) usage() (count, bytes int64, err error) {
	for _, dir := range []string{s.inbound, s.retry, s.legacyPending} {
		entries, e := os.ReadDir(dir)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return 0, 0, e
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			msg, e := s.read(filepath.Join(dir, entry.Name()))
			if e != nil {
				return 0, 0, e
			}
			count++
			bytes += int64(len(msg.Data))
		}
	}
	return count, bytes, nil
}

// admit runs with mu held through the subsequent atomic write. This critical
// section is the reservation: failure releases it without a persistent counter.
// Scanning authoritative files also restores usage after restart and transitions.
func (s *Store) admit(msg *model.Message) error {
	if !s.limits.enabled() {
		return nil
	}
	count, bytes, err := s.usage()
	if err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.SetGauge("queue_pending_messages", count)
		s.metrics.SetGauge("queue_pending_bytes", bytes)
	}
	l := s.limits
	low := l.LowPercent
	if low == 0 {
		low = 80
	}
	below := func(n, max int64) bool { return max == 0 || n <= max/100*low+max%100*low/100 }
	diskOK := true
	if l.MinFreeBytes > 0 {
		free, err := s.freeBytes(s.root)
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(msg, "", "  ")
		if err != nil {
			return err
		}
		diskOK = free >= uint64(l.MinFreeBytes) && uint64(len(encoded)) <= free-uint64(l.MinFreeBytes)
		if s.metrics != nil {
			s.metrics.SetGauge("queue_disk_available_bytes", int64(free))
		}
	}
	if s.paused && below(count, l.MaxMessages) && below(bytes, l.MaxBytes) && diskOK {
		s.paused = false
		if s.metrics != nil {
			s.metrics.Counter("queue_capacity_resumed").Inc()
		}
	}
	if s.paused || (l.MaxMessages > 0 && count >= l.MaxMessages) || (l.MaxBytes > 0 && int64(len(msg.Data)) > l.MaxBytes-bytes) || !diskOK {
		s.paused = true
		if s.metrics != nil {
			s.metrics.Counter("queue_capacity_rejected").Inc()
			s.metrics.SetGauge("queue_capacity_paused", 1)
		}
		return ErrCapacity
	}
	if s.metrics != nil {
		s.metrics.SetGauge("queue_capacity_paused", 0)
	}
	return nil
}

// refreshCapacityMetrics is called under the store lock after queue operations.
func (s *Store) refreshCapacityMetrics() {
	if s.metrics == nil || !s.limits.enabled() {
		return
	}
	count, bytes, err := s.usage()
	if err != nil {
		s.metrics.Counter("queue_capacity_scan_errors").Inc()
		return
	}
	s.metrics.SetGauge("queue_pending_messages", count)
	s.metrics.SetGauge("queue_pending_bytes", bytes)
	paused := int64(0)
	if s.paused {
		paused = 1
	}
	s.metrics.SetGauge("queue_capacity_paused", paused)
}
