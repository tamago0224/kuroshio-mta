package router

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

type DNSCacheConfig struct {
	Enabled     bool
	PositiveTTL time.Duration
	NegativeTTL time.Duration
	Capacity    int
}

type cacheEntry[T any] struct {
	value   T
	err     error
	expires time.Time
	used    time.Time
}

type lookupCall[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// CachedResolver applies explicit operator TTLs to MX and address lookups.
// It does not claim to preserve authoritative DNS TTLs because net.Resolver
// does not expose them.
type CachedResolver struct {
	cfg     DNSCacheConfig
	metrics *observability.Metrics
	now     func() time.Time

	lookupMX   func(context.Context, string) ([]*net.MX, error)
	lookupHost func(context.Context, string) ([]string, error)

	mu        sync.Mutex
	mx        map[string]cacheEntry[[]MXHost]
	host      map[string]cacheEntry[[]string]
	mxCalls   map[string]*lookupCall[[]MXHost]
	hostCalls map[string]*lookupCall[[]string]
}

func NewCachedResolver(cfg DNSCacheConfig, metrics *observability.Metrics) *CachedResolver {
	if cfg.PositiveTTL <= 0 {
		cfg.PositiveTTL = time.Minute
	}
	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = 30 * time.Second
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 10000
	}
	return &CachedResolver{
		cfg: cfg, metrics: metrics, now: time.Now,
		lookupMX: net.DefaultResolver.LookupMX, lookupHost: net.DefaultResolver.LookupHost,
		mx: map[string]cacheEntry[[]MXHost]{}, host: map[string]cacheEntry[[]string]{},
		mxCalls: map[string]*lookupCall[[]MXHost]{}, hostCalls: map[string]*lookupCall[[]string]{},
	}
}

func (r *CachedResolver) LookupWithTimeout(domain string, timeout time.Duration) ([]MXHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.LookupMX(ctx, domain)
}

func (r *CachedResolver) LookupMX(ctx context.Context, domain string) ([]MXHost, error) {
	key := normalizeDNSName(domain)
	if !r.cfg.Enabled {
		return r.lookupMXAndFallback(ctx, key)
	}
	r.mu.Lock()
	now := r.now()
	if entry, ok := r.mx[key]; ok && entry.expires.After(now) {
		entry.used = now
		r.mx[key] = entry
		r.mu.Unlock()
		r.inc("dns_cache_mx_hit")
		return cloneMX(entry.value), entry.err
	}
	if call := r.mxCalls[key]; call != nil {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			r.inc("dns_cache_mx_coalesced")
			return cloneMX(call.value), call.err
		}
	}
	call := &lookupCall[[]MXHost]{done: make(chan struct{})}
	r.mxCalls[key] = call
	r.mu.Unlock()
	r.inc("dns_cache_mx_miss")
	started := time.Now()
	value, err := r.lookupMXAndFallback(ctx, key)
	r.add("dns_cache_mx_lookup_milliseconds", uint64(time.Since(started).Milliseconds()))
	if err != nil {
		r.inc("dns_cache_mx_error")
	}
	r.mu.Lock()
	call.value, call.err = cloneMX(value), err
	delete(r.mxCalls, key)
	if cacheableDNSResult(ctx, err) {
		r.evictMXLocked(now)
		ttl := r.cfg.PositiveTTL
		if err != nil {
			ttl = r.cfg.NegativeTTL
		}
		r.mx[key] = cacheEntry[[]MXHost]{value: cloneMX(value), err: err, expires: now.Add(ttl), used: now}
	}
	close(call.done)
	r.mu.Unlock()
	return value, err
}

func (r *CachedResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	key := normalizeDNSName(host)
	if !r.cfg.Enabled {
		return r.lookupHost(ctx, key)
	}
	r.mu.Lock()
	now := r.now()
	if entry, ok := r.host[key]; ok && entry.expires.After(now) {
		entry.used = now
		r.host[key] = entry
		r.mu.Unlock()
		r.inc("dns_cache_address_hit")
		return append([]string(nil), entry.value...), entry.err
	}
	if call := r.hostCalls[key]; call != nil {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			r.inc("dns_cache_address_coalesced")
			return append([]string(nil), call.value...), call.err
		}
	}
	call := &lookupCall[[]string]{done: make(chan struct{})}
	r.hostCalls[key] = call
	r.mu.Unlock()
	r.inc("dns_cache_address_miss")
	started := time.Now()
	value, err := r.lookupHost(ctx, key)
	r.add("dns_cache_address_lookup_milliseconds", uint64(time.Since(started).Milliseconds()))
	if err != nil {
		r.inc("dns_cache_address_error")
	}
	r.mu.Lock()
	call.value, call.err = append([]string(nil), value...), err
	delete(r.hostCalls, key)
	if cacheableDNSResult(ctx, err) {
		r.evictHostLocked(now)
		ttl := r.cfg.PositiveTTL
		if err != nil {
			ttl = r.cfg.NegativeTTL
		}
		r.host[key] = cacheEntry[[]string]{value: append([]string(nil), value...), err: err, expires: now.Add(ttl), used: now}
	}
	close(call.done)
	r.mu.Unlock()
	return value, err
}

func (r *CachedResolver) lookupMXAndFallback(ctx context.Context, domain string) ([]MXHost, error) {
	mx, err := r.lookupMX(ctx, domain)
	if err != nil || len(mx) == 0 {
		addrs, addressErr := r.LookupHost(ctx, domain)
		if addressErr != nil || len(addrs) == 0 {
			if err != nil {
				return nil, err
			}
			return nil, addressErr
		}
		return []MXHost{{Host: domain}}, nil
	}
	hosts := make([]MXHost, 0, len(mx))
	for _, item := range mx {
		hosts = append(hosts, MXHost{Host: strings.TrimSuffix(item.Host, "."), Pref: item.Pref})
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Pref == hosts[j].Pref {
			return hosts[i].Host < hosts[j].Host
		}
		return hosts[i].Pref < hosts[j].Pref
	})
	return hosts, nil
}

func (r *CachedResolver) evictMXLocked(now time.Time) {
	for key, entry := range r.mx {
		if !entry.expires.After(now) {
			delete(r.mx, key)
		}
	}
	for len(r.mx) >= r.cfg.Capacity {
		delete(r.mx, oldestMX(r.mx))
	}
}

func (r *CachedResolver) evictHostLocked(now time.Time) {
	for key, entry := range r.host {
		if !entry.expires.After(now) {
			delete(r.host, key)
		}
	}
	for len(r.host) >= r.cfg.Capacity {
		delete(r.host, oldestHost(r.host))
	}
}

func oldestMX(entries map[string]cacheEntry[[]MXHost]) string {
	var key string
	var used time.Time
	for candidate, entry := range entries {
		if key == "" || entry.used.Before(used) {
			key, used = candidate, entry.used
		}
	}
	return key
}

func oldestHost(entries map[string]cacheEntry[[]string]) string {
	var key string
	var used time.Time
	for candidate, entry := range entries {
		if key == "" || entry.used.Before(used) {
			key, used = candidate, entry.used
		}
	}
	return key
}

func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func cacheableDNSResult(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var networkError net.Error
	return !errors.As(err, &networkError) || !networkError.Timeout()
}

func cloneMX(value []MXHost) []MXHost { return append([]MXHost(nil), value...) }
func (r *CachedResolver) inc(name string) {
	if r.metrics != nil {
		r.metrics.Counter(name).Inc()
	}
}

func (r *CachedResolver) add(name string, value uint64) {
	if r.metrics != nil {
		r.metrics.Counter(name).Add(value)
	}
}
