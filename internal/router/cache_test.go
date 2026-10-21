package router

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

func TestCachedResolverPositiveAndTTLBoundary(t *testing.T) {
	now := time.Unix(100, 0)
	var calls atomic.Int64
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Second, Capacity: 10}, observability.NewMetrics())
	r.now = func() time.Time { return now }
	r.lookupMX = func(context.Context, string) ([]*net.MX, error) {
		call := calls.Add(1)
		return []*net.MX{{Host: fmt.Sprintf("mx%d.example.test.", call), Pref: 10}}, nil
	}
	first, err := r.LookupMX(context.Background(), "Example.Test.")
	if err != nil {
		t.Fatal(err)
	}
	first[0].Host = "mutated"
	second, err := r.LookupMX(context.Background(), "example.test")
	if err != nil || second[0].Host != "mx1.example.test" || calls.Load() != 1 {
		t.Fatalf("cached lookup = %v, %v, calls=%d", second, err, calls.Load())
	}
	now = now.Add(time.Minute)
	refreshed, err := r.LookupMX(context.Background(), "example.test")
	if err != nil || refreshed[0].Host != "mx2.example.test" || calls.Load() != 2 {
		t.Fatalf("TTL boundary did not refresh: %v, %v calls=%d", refreshed, err, calls.Load())
	}
}

func TestCachedResolverMXFallsBackToAddress(t *testing.T) {
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Minute, Capacity: 10}, nil)
	r.lookupMX = func(context.Context, string) ([]*net.MX, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "fallback.test", IsNotFound: true}
	}
	r.lookupHost = func(context.Context, string) ([]string, error) {
		return []string{"192.0.2.1", "2001:db8::1"}, nil
	}
	hosts, err := r.LookupMX(context.Background(), "fallback.test")
	if err != nil || len(hosts) != 1 || hosts[0].Host != "fallback.test" || hosts[0].Pref != 0 {
		t.Fatalf("fallback lookup = %v, %v", hosts, err)
	}
}

func TestCachedResolverNegativeAndConcurrentMiss(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Minute, Capacity: 10}, nil)
	r.lookupMX = func(context.Context, string) ([]*net.MX, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil, errors.New("NXDOMAIN")
	}
	r.lookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("NXDOMAIN") }
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, _ = r.LookupMX(context.Background(), "missing.test")
		})
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent misses performed %d MX lookups", calls.Load())
	}
	if _, err := r.LookupMX(context.Background(), "missing.test"); err == nil || calls.Load() != 1 {
		t.Fatalf("negative cache miss: %v calls=%d", err, calls.Load())
	}
}

func TestCachedResolverAddressCapacityAndCancellation(t *testing.T) {
	now := time.Unix(100, 0)
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Minute, Capacity: 1}, nil)
	r.now = func() time.Time { return now }
	var calls atomic.Int64
	r.lookupHost = func(ctx context.Context, host string) ([]string, error) {
		calls.Add(1)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []string{"192.0.2.1"}, nil
	}
	if _, err := r.LookupHost(context.Background(), "one.test"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := r.LookupHost(context.Background(), "two.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LookupHost(context.Background(), "one.test"); err != nil || calls.Load() != 3 {
		t.Fatalf("capacity eviction failed: %v calls=%d", err, calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.LookupHost(ctx, "canceled.test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestCachedResolverDoesNotCacheTimeout(t *testing.T) {
	var calls atomic.Int64
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Minute, Capacity: 10}, nil)
	r.lookupHost = func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return nil, &net.DNSError{Err: "timeout", Name: "slow.test", IsTimeout: true, IsTemporary: true}
	}
	for range 2 {
		if _, err := r.LookupHost(context.Background(), "slow.test"); err == nil {
			t.Fatal("timeout lookup unexpectedly succeeded")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("timeout was cached: calls=%d", calls.Load())
	}
}

func TestCachedResolverRestartStartsEmpty(t *testing.T) {
	var calls atomic.Int64
	lookup := func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return []string{"192.0.2.1"}, nil
	}
	config := DNSCacheConfig{Enabled: true, PositiveTTL: time.Minute, NegativeTTL: time.Minute, Capacity: 10}
	first := NewCachedResolver(config, nil)
	first.lookupHost = lookup
	if _, err := first.LookupHost(context.Background(), "mx.example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.LookupHost(context.Background(), "mx.example.test"); err != nil {
		t.Fatal(err)
	}
	second := NewCachedResolver(config, nil)
	second.lookupHost = lookup
	if _, err := second.LookupHost(context.Background(), "mx.example.test"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("new resolver reused process cache: calls=%d", calls.Load())
	}
}

func BenchmarkCachedResolverHit(b *testing.B) {
	r := NewCachedResolver(DNSCacheConfig{Enabled: true, PositiveTTL: time.Hour, Capacity: 100}, nil)
	r.lookupHost = func(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }
	if _, err := r.LookupHost(context.Background(), "mx.example.test"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.LookupHost(context.Background(), "mx.example.test"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolverWithoutCache(b *testing.B) {
	r := NewCachedResolver(DNSCacheConfig{Enabled: false}, nil)
	r.lookupHost = func(context.Context, string) ([]string, error) {
		time.Sleep(time.Millisecond)
		return []string{"192.0.2.1"}, nil
	}
	for b.Loop() {
		if _, err := r.LookupHost(context.Background(), "mx.example.test"); err != nil {
			b.Fatal(err)
		}
	}
}
