package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type persistedState struct {
	Version int                     `json:"version"`
	Domains map[string]*domainStats `json:"domains"`
	Seen    map[string]time.Time    `json:"seen_reports"`
}

// NewPersistent restores a local tracker; callers must periodically Flush it.
// One process must own the state file. Delivery observations have a bounded RPO
// determined by the flush interval; successful report ingestion flushes immediately.
func NewPersistent(cfg Config, path string) (*Tracker, error) {
	if path == "" {
		return nil, errors.New("reputation state path is required")
	}
	t := New(cfg)
	t.statePath = path
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 64<<20 {
		return nil, errors.New("reputation state exceeds size limit")
	}
	var state persistedState
	decoder := json.NewDecoder(io.LimitReader(f, 64<<20))
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("read reputation state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing reputation state data")
	}
	if state.Version != 1 {
		return nil, errors.New("unsupported reputation state version")
	}
	for domain, stats := range state.Domains {
		if !validReportDomain(domain) || stats == nil || stats.TodayAttempts < 0 {
			return nil, errors.New("invalid reputation state")
		}
	}
	if state.Domains != nil {
		t.domains = state.Domains
	}
	if state.Seen != nil {
		t.seen = state.Seen
	}
	return t, nil
}

func (t *Tracker) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.flushLocked()
}

func (t *Tracker) flushLocked() error {
	if t.statePath == "" || !t.dirty {
		return nil
	}
	data, err := json.Marshal(persistedState{Version: 1, Domains: t.domains, Seen: t.seen})
	if err != nil {
		return err
	}
	if len(data) > 64<<20 {
		return errors.New("reputation state exceeds size limit")
	}
	dir := filepath.Dir(t.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".reputation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), t.statePath); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return err
	}
	t.dirty = false
	return nil
}

func (t *Tracker) RunPersistence(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("reputation flush interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := t.Flush(); err != nil {
				return fmt.Errorf("persist reputation: %w", err)
			}
		}
	}
}

// Ingest applies a validated report atomically with its deduplication marker.
func (t *Tracker) Ingest(report Report, now time.Time) (bool, error) {
	if report.ID == "" || len(report.Counts) == 0 || len(report.Counts) > 100 {
		return false, errors.New("invalid report")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, at := range t.seen {
		if now.Sub(at) > 30*24*time.Hour {
			delete(t.seen, id)
			t.dirty = true
		}
	}
	if _, ok := t.seen[report.ID]; ok {
		t.ingestion["duplicate"]++
		return false, nil
	}
	previous := make(map[string]*domainStats, len(report.Counts))
	for domain, counts := range report.Counts {
		if !validReportDomain(domain) {
			return false, errors.New("invalid report domain")
		}
		old := t.domains[domain]
		if old != nil {
			saved := *old
			previous[domain] = &saved
			overflow := ^uint64(0)-old.Complaints < counts.Complaints ||
				^uint64(0)-old.TLSRPTSuccess < counts.Successes || ^uint64(0)-old.TLSRPTFailure < counts.Failures
			if overflow {
				return false, errors.New("report count overflow")
			}
		} else {
			previous[domain] = nil
		}
	}
	for domain, counts := range report.Counts {
		stats := t.getLocked(domain, now)
		stats.Complaints += counts.Complaints
		stats.TLSRPTSuccess += counts.Successes
		stats.TLSRPTFailure += counts.Failures
	}
	t.seen[report.ID] = now
	t.dirty = true
	if err := t.flushLocked(); err != nil {
		for domain, stats := range previous {
			if stats == nil {
				delete(t.domains, domain)
			} else {
				t.domains[domain] = stats
			}
		}
		delete(t.seen, report.ID)
		t.ingestion["error"]++
		return false, err
	}
	t.ingestion["accepted"]++
	return true, nil
}

func (t *Tracker) RejectReport() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ingestion["rejected"]++
}

func (t *Tracker) RenderReportMetrics() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out string
	for _, result := range []string{"accepted", "duplicate", "rejected", "error"} {
		out += fmt.Sprintf("reputation_report_ingest_total{result=%q} %d\n", result, t.ingestion[result])
	}
	return out
}
