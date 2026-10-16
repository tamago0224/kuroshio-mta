package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func limitedStore(t *testing.T, limits CapacityLimits) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.limits = limits
	s.freeBytes = func(string) (uint64, error) { return 1 << 30, nil }
	return s
}

func TestCapacityConcurrentAdmission(t *testing.T) {
	s := limitedStore(t, CapacityLimits{MaxMessages: 10})
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			err := s.Enqueue(&model.Message{ID: fmt.Sprint(i)})
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrCapacity) {
				t.Errorf("enqueue: %v", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted %d, want 10", accepted.Load())
	}
}

func TestCapacityRecoveryAndRestart(t *testing.T) {
	s := limitedStore(t, CapacityLimits{MaxMessages: 2, MaxBytes: 10, LowPercent: 50})
	a := &model.Message{ID: "a", Data: []byte("12345")}
	b := &model.Message{ID: "b", Data: []byte("12345")}
	for _, msg := range []*model.Message{a, b} {
		if err := s.Enqueue(msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Enqueue(a); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.limits = s.limits
	if err := reopened.Enqueue(&model.Message{ID: "c"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("restart: %v", err)
	}
	if err := s.Retry(a, time.Hour, "later"); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "c"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("retry released capacity: %v", err)
	}
	if err := s.AckSent(a.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "c", Data: []byte("12345")}); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if err := s.Fail(b, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequeueFromState("dlq", b.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequeueFromState("sent", a.ID, time.Now()); !errors.Is(err, ErrCapacity) {
		t.Fatalf("requeue bypassed limit: %v", err)
	}
}

func TestCapacityFailedWriteReleasesReservation(t *testing.T) {
	s := limitedStore(t, CapacityLimits{MaxMessages: 1})
	msg := &model.Message{ID: "a"}
	// An existing temporary directory makes the atomic write fail deterministically.
	tmp := filepath.Join(s.inbound, "a.json.tmp")
	if err := os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "block"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(msg); err == nil {
		t.Fatal("expected save error")
	}
	if err := s.Enqueue(&model.Message{ID: "b"}); err != nil {
		t.Fatalf("reservation leaked: %v", err)
	}
}

func TestCapacityDiskAndWatermark(t *testing.T) {
	s := limitedStore(t, CapacityLimits{MaxMessages: 4, LowPercent: 50, MinFreeBytes: 100})
	for i := range 4 {
		if err := s.Enqueue(&model.Message{ID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Enqueue(&model.Message{ID: "extra"}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if err := s.AckSent("0", &model.Message{ID: "0"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "extra"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("resumed above low watermark")
	}
	if err := s.AckSent("1", &model.Message{ID: "1"}); err != nil {
		t.Fatal(err)
	}
	s.freeBytes = func(string) (uint64, error) { return 100, nil }
	if err := s.Enqueue(&model.Message{ID: "extra"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("ignored disk reserve")
	}
	s.freeBytes = func(string) (uint64, error) { return 1 << 30, nil }
	if err := s.Enqueue(&model.Message{ID: "extra"}); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityUnsupportedAndInvalid(t *testing.T) {
	for _, cfg := range []config.Config{
		{QueueBackend: "kafka", QueueMaxMessages: 1},
		{QueueBackend: "local", QueueMaxBytes: -1},
		{QueueBackend: "local", QueueLowWatermarkPercent: 100},
	} {
		if _, err := NewBackend(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}

func TestCapacityExclusiveDirectory(t *testing.T) {
	cfg := config.Config{QueueBackend: "local", QueueDir: t.TempDir(), QueueMaxMessages: 1}
	first, err := NewBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := NewBackend(cfg); err == nil {
		other.Close()
		t.Fatal("second owner accepted")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestCapacityBytesBoundaryAndFailedTransition(t *testing.T) {
	s := limitedStore(t, CapacityLimits{MaxBytes: 5})
	if err := s.Enqueue(&model.Message{ID: "large", Data: []byte("123456")}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	msg := &model.Message{ID: "fits", Data: []byte("12345")}
	if err := s.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	// Destination failure must preserve the accepted message and its quota usage.
	if err := os.Remove(s.sent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.sent, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.AckSent(msg.ID, msg); err == nil {
		t.Fatal("expected transition failure")
	}
	count, bytes, err := s.usage()
	if err != nil || count != 1 || bytes != 5 {
		t.Fatalf("usage %d/%d: %v", count, bytes, err)
	}
}
