package ingress

import (
	"sync"
	"time"
)

type LimitStore interface {
	Allow(namespace, key string, now time.Time, limit int, window time.Duration) (bool, error)
}

type RateLimiter struct {
	name   string
	limit  int
	window time.Duration
	store  LimitStore
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return NewRateLimiterWithStore("default", limit, window, nil)
}

func NewRateLimiterWithStore(name string, limit int, window time.Duration, store LimitStore) *RateLimiter {
	if store == nil {
		store = NewLocalLimitStore()
	}
	return &RateLimiter{
		name:   name,
		limit:  limit,
		window: window,
		store:  store,
	}
}

func (r *RateLimiter) Allow(key string, now time.Time) (bool, error) {
	if r == nil || r.limit <= 0 {
		return true, nil
	}
	return r.store.Allow(r.name, key, now, r.limit, r.window)
}

type localLimitStore struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewLocalLimitStore() LimitStore {
	return &localLimitStore{hits: map[string][]time.Time{}}
}

func (l *localLimitStore) Allow(namespace, key string, now time.Time, limit int, window time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	storeKey := namespace + "\x00" + key
	windowStart := now.Add(-window)
	arr := l.hits[storeKey]
	kept := arr[:0]
	for _, ts := range arr {
		if !ts.Before(windowStart) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= limit {
		l.hits[storeKey] = kept
		return false, nil
	}
	kept = append(kept, now)
	l.hits[storeKey] = kept
	return true, nil
}
