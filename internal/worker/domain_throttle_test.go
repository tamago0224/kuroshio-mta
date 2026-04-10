package worker

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestParseDomainRules(t *testing.T) {
	got := parseDomainRules("gmail.com:2, yahoo.com:1,invalid,nope:x")
	if got["gmail.com"] != 2 || got["yahoo.com"] != 1 {
		t.Fatalf("unexpected rules: %#v", got)
	}
	if _, ok := got["invalid"]; ok {
		t.Fatalf("invalid rule must be ignored: %#v", got)
	}
}

func TestDomainThrottleConcurrencyLimit(t *testing.T) {
	th := newLocalDomainThrottle(1, map[string]int{"gmail.com": 2}, false, 0.3, time.Second)
	lease1 := th.acquire("gmail.com")
	lease2 := th.acquire("gmail.com")
	release1 := lease1.release
	release2 := lease2.release

	var done int32
	go func() {
		lease3 := th.acquire("gmail.com")
		atomic.StoreInt32(&done, 1)
		lease3.release()
	}()
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&done) != 0 {
		t.Fatal("third acquire must block by concurrency limit")
	}
	release1()
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&done) != 1 {
		t.Fatal("third acquire must proceed after release")
	}
	release2()
}

func TestDomainThrottleAdaptivePenalty(t *testing.T) {
	th := newLocalDomainThrottle(2, nil, true, 0.3, time.Second)
	for i := 0; i < 20; i++ {
		th.observe("example.com", true)
	}
	st := th.get("example.com")
	if p := th.currentPenalty(st); p == 0 {
		t.Fatal("penalty must increase after high temporary failure ratio")
	}
}

func TestLocalDomainThrottleAcquireReportsWaitDuration(t *testing.T) {
	th := newLocalDomainThrottle(1, nil, false, 0.3, time.Second)
	lease := th.acquire("example.com")
	if lease.waitDuration < 0 {
		t.Fatalf("wait duration must be non-negative: %s", lease.waitDuration)
	}
	if lease.backendError {
		t.Fatal("local throttle should not report backend error")
	}
	lease.release()
}

func TestNewDomainThrottleDefaultsToMemory(t *testing.T) {
	cfg := config.Config{
		DomainThrottleBackend:      "memory",
		DomainMaxConcurrentDefault: 2,
		DomainAdaptiveThrottle:     true,
		DomainTempFailThreshold:    0.3,
		DomainPenaltyMax:           time.Second,
	}

	th, err := newDomainThrottle(cfg)
	if err != nil {
		t.Fatalf("newDomainThrottle() error: %v", err)
	}
	if _, ok := th.(*localDomainThrottle); !ok {
		t.Fatalf("expected localDomainThrottle, got %T", th)
	}
}

func TestNewDomainThrottleRedisRequiresAddrs(t *testing.T) {
	cfg := config.Config{
		DomainThrottleBackend:      "redis",
		DomainMaxConcurrentDefault: 2,
		DomainAdaptiveThrottle:     true,
		DomainTempFailThreshold:    0.3,
		DomainPenaltyMax:           time.Second,
	}

	_, err := newDomainThrottle(cfg)
	if err == nil {
		t.Fatal("expected redis backend error")
	}
	if !strings.Contains(err.Error(), "requires at least one address") {
		t.Fatalf("unexpected error: %v", err)
	}
}
