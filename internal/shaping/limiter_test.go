package shaping

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestMemorySharedLimitsAndRecovery(t *testing.T) {
	clock := time.Now()
	l := newMemoryLimiter()
	l.now = func() time.Time { return clock }
	limits := config.ShapingLimits{MessagesPerMinute: 60, MessageBurst: 2, ConnectionsPerMinute: 60, ConnectionBurst: 2, MaxConnections: 1}
	release, delay, err := l.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("first: %s %v", delay, err)
	}
	_, delay, err = l.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay <= 0 {
		t.Fatalf("concurrency ignored: %s %v", delay, err)
	}
	other, delay, err := l.acquire(context.Background(), "other", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("other group blocked: %s %v", delay, err)
	}
	other()
	release()
	release()
	release, delay, err = l.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("release: %s %v", delay, err)
	}
	release()
	_, delay, err = l.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay != time.Second {
		t.Fatalf("burst: %s %v", delay, err)
	}
	clock = clock.Add(time.Second)
	release, delay, err = l.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("refill: %s %v", delay, err)
	}
	release()
}

func TestMemoryConcurrentAdmissionAndCancel(t *testing.T) {
	l := newMemoryLimiter()
	limits := config.ShapingLimits{MessagesPerMinute: 1, MessageBurst: 5}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			release, wait, err := l.acquire(context.Background(), "group", limits, true, time.Minute)
			if err != nil {
				t.Error(err)
			}
			if wait == 0 {
				admitted.Add(1)
				release()
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 5 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := l.acquire(ctx, "group", limits, true, time.Minute); err == nil {
		t.Fatal("cancellation ignored")
	}
}
