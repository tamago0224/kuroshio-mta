package shaping

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

type limiter interface {
	acquire(context.Context, string, config.ShapingLimits, bool, time.Duration) (func(), time.Duration, error)
	close() error
}

type bucket struct {
	tokens  float64
	updated time.Time
}

func (b bucket) refill(now time.Time, rate, burst int) bucket {
	if rate == 0 {
		return b
	}
	if burst == 0 {
		burst = 1
	}
	if b.updated.IsZero() {
		b.tokens = float64(burst)
	} else {
		b.tokens = math.Min(float64(burst), b.tokens+math.Max(0, now.Sub(b.updated).Seconds())*float64(rate)/60)
	}
	b.updated = now
	return b
}
func (b bucket) delay(rate int) time.Duration {
	if rate == 0 || b.tokens >= 1 {
		return 0
	}
	return time.Duration(math.Ceil((1 - b.tokens) * 60 / float64(rate) * float64(time.Second)))
}

type memoryState struct {
	messages, connections bucket
	leases                map[uint64]time.Time
	last                  time.Time
	idleTTL               time.Duration
}
type memoryLimiter struct {
	mu     sync.Mutex
	states map[string]*memoryState
	seq    uint64
	now    func() time.Time
}

func newMemoryLimiter() *memoryLimiter {
	return &memoryLimiter{states: map[string]*memoryState{}, now: time.Now}
}
func (*memoryLimiter) close() error { return nil }

func idleTTL(l config.ShapingLimits, leaseTTL time.Duration) time.Duration {
	ttl := leaseTTL + time.Minute
	for _, pair := range [][2]int{{l.MessagesPerMinute, l.MessageBurst}, {l.ConnectionsPerMinute, l.ConnectionBurst}} {
		if pair[0] > 0 {
			burst := max(pair[1], 1)
			ttl = max(ttl, time.Duration(math.Ceil(float64(burst)*60/float64(pair[0])))*time.Second+time.Minute)
		}
	}
	return ttl
}

func (l *memoryLimiter) acquire(ctx context.Context, key string, limits config.ShapingLimits, message bool, ttl time.Duration) (func(), time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	now := l.now()
	s := l.states[key]
	if s == nil {
		if len(l.states) >= 10000 {
			for k, v := range l.states {
				if now.Sub(v.last) > v.idleTTL {
					delete(l.states, k)
				}
			}
		}
		if len(l.states) >= 10000 {
			return nil, 0, errors.New("MX shaping group state capacity exceeded")
		}
		s = &memoryState{leases: map[uint64]time.Time{}, idleTTL: idleTTL(limits, ttl)}
		l.states[key] = s
	}
	s.last = now
	for token, expiry := range s.leases {
		if !expiry.After(now) {
			delete(s.leases, token)
		}
	}
	s.messages = s.messages.refill(now, limits.MessagesPerMinute, limits.MessageBurst)
	s.connections = s.connections.refill(now, limits.ConnectionsPerMinute, limits.ConnectionBurst)
	wait := s.connections.delay(limits.ConnectionsPerMinute)
	if message {
		wait = max(wait, s.messages.delay(limits.MessagesPerMinute))
	}
	if limits.MaxConnections > 0 && len(s.leases) >= limits.MaxConnections {
		wait = max(wait, 100*time.Millisecond)
	}
	if wait > 0 {
		return nil, wait, nil
	}
	if message && limits.MessagesPerMinute > 0 {
		s.messages.tokens--
	}
	if limits.ConnectionsPerMinute > 0 {
		s.connections.tokens--
	}
	l.seq++
	token := l.seq
	s.leases[token] = now.Add(ttl)
	return sync.OnceFunc(func() { l.mu.Lock(); delete(s.leases, token); l.mu.Unlock() }), 0, nil
}
