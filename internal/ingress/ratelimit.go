package ingress

import (
	"sync"
	"time"
)

type RateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:  limit,
		window: window,
		hits:   map[string][]time.Time{},
	}
}

func (r *RateLimiter) Allow(key string, now time.Time) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	windowStart := now.Add(-r.window)
	arr := r.hits[key]
	kept := arr[:0]
	for _, ts := range arr {
		if !ts.Before(windowStart) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= r.limit {
		r.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	r.hits[key] = kept
	return true
}
