package shaping

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/router"
)

func TestCanonicalGroupsAndPrecedence(t *testing.T) {
	cfg := config.Config{MXShaping: config.MXShapingConfig{Enabled: true, Default: config.ShapingLimits{MaxConnections: 7}, Providers: []config.ShapingProvider{
		{Name: "suffix", MXSuffixes: []string{"provider.test"}, Limits: config.ShapingLimits{MaxConnections: 2}},
		{Name: "explicit", Domains: []string{"customer.test"}, Limits: config.ShapingLimits{MaxConnections: 3}},
	}}}
	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hosts := []router.MXHost{{Host: "MX2.PROVIDER.TEST.", Pref: 10}, {Host: "mx1.provider.test", Pref: 20}}
	key, limits := s.Group("customer.test", hosts)
	if key != "provider:explicit" || limits.MaxConnections != 3 {
		t.Fatalf("domain priority %s %+v", key, limits)
	}
	key, limits = s.Group("other.test", hosts)
	if key != "provider:suffix" || limits.MaxConnections != 2 {
		t.Fatalf("suffix priority %s %+v", key, limits)
	}
	a, _ := s.Group("a.test", []router.MXHost{{Host: "mx.a.test"}, {Host: "MX.B.TEST."}})
	b, _ := s.Group("b.test", []router.MXHost{{Host: "mx.b.test", Pref: 100}, {Host: "mx.a.test"}, {Host: "mx.a.test"}})
	if a != b {
		t.Fatal("MX set order, priority, or duplicate affected group")
	}
	c, _ := s.Group("c.test", []router.MXHost{{Host: "other.test"}})
	if a == c {
		t.Fatal("different MX groups collided")
	}
	implicitA, _ := s.Group("a.test", []router.MXHost{{Host: "a.test"}})
	implicitB, _ := s.Group("b.test", []router.MXHost{{Host: "b.test"}})
	if implicitA == implicitB {
		t.Fatal("implicit MX domains incorrectly merged")
	}
}

func TestMXCacheExpiryChangeAndFailure(t *testing.T) {
	s, err := New(config.Config{MXShaping: config.MXShapingConfig{Enabled: true, CacheTTL: time.Second}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	calls := 0
	failure := false
	lookup := func(string, time.Duration) ([]router.MXHost, error) {
		calls++
		if failure {
			return nil, errors.New("DNS down")
		}
		host := "old.test"
		if calls > 1 {
			host = "new.test"
		}
		return []router.MXHost{{Host: host}}, nil
	}
	first, err := s.Lookup(context.Background(), "a.test", lookup, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.Group("a.test", first)
	first[0].Host = "mutated"
	cached, err := s.Lookup(context.Background(), "a.test", lookup, time.Second)
	if err != nil || cached[0].Host != "old.test" || calls != 1 {
		t.Fatalf("cache alias or miss %v %v", cached, err)
	}
	now = now.Add(time.Second)
	changed, err := s.Lookup(context.Background(), "a.test", lookup, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.Group("a.test", changed)
	if fresh == old {
		t.Fatal("MX change not reflected at expiry")
	}
	failure = true
	now = now.Add(time.Second)
	if _, err := s.Lookup(context.Background(), "a.test", lookup, time.Second); err == nil {
		t.Fatal("served stale MX after DNS failure")
	}
	failure = false
	if _, err := s.Lookup(context.Background(), "a.test", lookup, time.Second); err != nil || calls != 4 {
		t.Fatal("cached failure")
	}
}

type brokenLimiter struct{}

func (brokenLimiter) acquire(context.Context, string, config.ShapingLimits, bool, time.Duration) (func(), time.Duration, error) {
	return nil, 0, errors.New("unavailable")
}
func (brokenLimiter) close() error { return nil }
func TestBackendFailureDefersAndCancelDoesNotDebit(t *testing.T) {
	m := observability.NewMetrics()
	s, err := New(config.Config{MXShaping: config.MXShapingConfig{Enabled: true}}, m)
	if err != nil {
		t.Fatal(err)
	}
	s.limiter = brokenLimiter{}
	_, err = s.Acquire(context.Background(), "group", config.ShapingLimits{}, true)
	var deferred *DeferredError
	if !errors.As(err, &deferred) || deferred.Delay <= 0 {
		t.Fatalf("backend failure opened limit: %v", err)
	}
	if m.Snapshot()["mx_shaping_backend_error_total"] != 1 {
		t.Fatal("backend error metric missing")
	}
	s.limiter = newMemoryLimiter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Acquire(ctx, "group", config.ShapingLimits{MessagesPerMinute: 1}, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	release, err := s.Acquire(context.Background(), "group", config.ShapingLimits{MessagesPerMinute: 1}, true)
	if err != nil {
		t.Fatal("cancelled acquisition spent token")
	}
	release()
}
