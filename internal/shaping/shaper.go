package shaping

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/router"
)

// DeferredError requests a queue reschedule without consuming a delivery attempt.
type DeferredError struct {
	Delay  time.Duration
	Reason string
}

func (e *DeferredError) Error() string { return "MX shaping deferred: " + e.Reason }

type mxCacheEntry struct {
	hosts   []router.MXHost
	expires time.Time
}
type Shaper struct {
	cfg      config.MXShapingConfig
	limiter  limiter
	metrics  *observability.Metrics
	leaseTTL time.Duration
	mu       sync.Mutex
	cache    map[string]mxCacheEntry
	now      func() time.Time
}

func New(cfg config.Config, metrics *observability.Metrics) (*Shaper, error) {
	if !cfg.MXShaping.Enabled {
		return nil, nil
	}
	if mode := strings.ToLower(strings.TrimSpace(cfg.DeliveryMode)); mode != "" && mode != "mx" && mode != "relay" {
		return nil, fmt.Errorf("MX shaping requires mx or relay delivery mode")
	}
	if err := cfg.MXShaping.Validate(); err != nil {
		return nil, err
	}
	ttl := cfg.DialTimeout + cfg.SendTimeout + 30*time.Second
	if ttl < time.Minute {
		ttl = time.Minute
	}
	s := &Shaper{cfg: cfg.MXShaping, metrics: metrics, leaseTTL: ttl, limiter: newMemoryLimiter(), cache: map[string]mxCacheEntry{}, now: time.Now}
	if s.cfg.RetryDelay == 0 {
		s.cfg.RetryDelay = time.Second
	}
	if s.cfg.Backend == "redis" {
		l, err := newRedisLimiter(cfg)
		if err != nil {
			return nil, err
		}
		s.limiter = l
	}
	return s, nil
}
func (s *Shaper) Close() error {
	if s == nil {
		return nil
	}
	return s.limiter.close()
}
func (s *Shaper) inc(name string) {
	if s.metrics != nil {
		s.metrics.Counter("mx_shaping_" + name).Inc()
	}
}

// Lookup caches successful MX snapshots for an explicit operator TTL. The Go
// resolver does not expose DNS RR TTLs; zero means no application caching.
func (s *Shaper) Lookup(ctx context.Context, domain string, lookup func(string, time.Duration) ([]router.MXHost, error), timeout time.Duration) ([]router.MXHost, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := normalize(domain)
	s.mu.Lock()
	entry, ok := s.cache[key]
	s.mu.Unlock()
	if ok && entry.expires.After(s.now()) {
		s.inc("mx_cache_hit")
		return append([]router.MXHost(nil), entry.hosts...), nil
	}
	hosts, err := lookup(domain, timeout)
	if err != nil {
		s.inc("mx_lookup_error")
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	valid := false
	for _, mx := range hosts {
		if normalize(mx.Host) != "" {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("MX shaping requires a nonempty MX result")
	}
	if s.cfg.CacheTTL > 0 {
		s.mu.Lock()
		if len(s.cache) >= 10000 {
			for k, v := range s.cache {
				if !v.expires.After(s.now()) {
					delete(s.cache, k)
				}
			}
		}
		if len(s.cache) < 10000 {
			s.cache[key] = mxCacheEntry{hosts: append([]router.MXHost(nil), hosts...), expires: s.now().Add(s.cfg.CacheTTL)}
		}
		s.mu.Unlock()
	}
	return hosts, nil
}

func normalize(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// Group gives explicit domain mappings priority over provider MX suffixes, then
// uses a canonical MX host set (ignoring priority and order). It is not an ACL.
func (s *Shaper) Group(domain string, hosts []router.MXHost) (string, config.ShapingLimits) {
	domain = normalize(domain)
	for _, p := range s.cfg.Providers {
		for _, d := range p.Domains {
			if normalize(d) == domain {
				return "provider:" + p.Name, p.Limits
			}
		}
	}
	for _, p := range s.cfg.Providers {
		for _, suffix := range p.MXSuffixes {
			suffix = normalize(suffix)
			for _, mx := range hosts {
				host := normalize(mx.Host)
				if host == suffix || strings.HasSuffix(host, "."+suffix) {
					return "provider:" + p.Name, p.Limits
				}
			}
		}
	}
	set := map[string]bool{}
	for _, host := range hosts {
		if h := normalize(host.Host); h != "" {
			set[h] = true
		}
	}
	names := make([]string, 0, len(set))
	for h := range set {
		names = append(names, h)
	}
	sort.Strings(names)
	return fmt.Sprintf("mx:%x", sha256.Sum256([]byte(strings.Join(names, "\n")))), s.cfg.Default
}

// Acquire never sleeps; rates count recipient attempts and actual TCP dial
// attempts separately. Rejected reservations consume neither bucket.
func (s *Shaper) Acquire(ctx context.Context, key string, limits config.ShapingLimits, message bool) (func(), error) {
	release, delay, err := s.limiter.acquire(ctx, "default:"+key, limits, message, s.leaseTTL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.inc("backend_error")
		slog.WarnContext(ctx, "MX shaping backend unavailable", "component", "delivery", "group", key)
		return nil, &DeferredError{Delay: s.cfg.RetryDelay, Reason: "backend_unavailable"}
	}
	if delay > 0 {
		s.inc("deferred")
		return nil, &DeferredError{Delay: max(delay, s.cfg.RetryDelay), Reason: "limit"}
	}
	s.inc("connection_admitted")
	if message {
		s.inc("message_admitted")
	}
	return release, nil
}
