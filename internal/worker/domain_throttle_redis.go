package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisDomainThrottlePrefix = "kuroshio:domainthrottle"
	redisDomainThrottleTimeout       = 2 * time.Second
	redisDomainThrottlePollInterval  = 50 * time.Millisecond
	redisDomainThrottleStatsTTL      = 24 * time.Hour
)

var redisDomainThrottleAcquireScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local ttl_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local token = ARGV[4]
local penalty_ms = tonumber(ARGV[5] or "0")
local gate = redis.call("ZSCORE", key, "__penalty_gate")
if penalty_ms > 0 and gate and tonumber(gate) > now then return 0 end

redis.call("ZREMRANGEBYSCORE", key, "-inf", now)
local count = redis.call("ZCARD", key)
if redis.call("ZSCORE", key, "__penalty_gate") then count = count - 1 end
if count >= limit then
  redis.call("PEXPIRE", key, ttl_ms)
  return 0
end

redis.call("ZADD", key, now + ttl_ms, token)
if penalty_ms > 0 then redis.call("ZADD", key, now + penalty_ms, "__penalty_gate") end
redis.call("PEXPIRE", key, math.max(ttl_ms, penalty_ms))
return 1
`)

var redisDomainThrottleObserveScript = redis.NewScript(`
local key = KEYS[1]
local temporary_failure = tonumber(ARGV[1])
local threshold = tonumber(ARGV[2])
local max_penalty_ms = tonumber(ARGV[3])
local ttl_ms = tonumber(ARGV[4])

local samples = redis.call("HINCRBY", key, "samples", 1)
local tempfails = redis.call("HINCRBY", key, "tempfails", temporary_failure)
local penalty_ms = tonumber(redis.call("HGET", key, "penalty_ms") or "0")

if samples >= 20 then
  local ratio = tempfails / samples
  if ratio >= threshold then
    if penalty_ms == 0 then
      penalty_ms = 200
    else
      penalty_ms = penalty_ms * 2
    end
    if penalty_ms > max_penalty_ms then
      penalty_ms = max_penalty_ms
    end
  else
    penalty_ms = math.floor(penalty_ms / 2)
    if penalty_ms < 50 then
      penalty_ms = 0
    end
  end
  redis.call("HSET", key, "samples", 0, "tempfails", 0, "penalty_ms", penalty_ms)
else
  redis.call("HSET", key, "penalty_ms", penalty_ms)
end

redis.call("PEXPIRE", key, ttl_ms)
return penalty_ms
`)

type redisDomainThrottleConfig struct {
	Addrs      []string
	Username   string
	Password   string
	DB         int
	KeyPrefix  string
	DefLimit   int
	Rules      map[string]int
	Adaptive   bool
	Threshold  float64
	MaxPenalty time.Duration
	LeaseTTL   time.Duration
}

type redisDomainThrottle struct {
	client     redis.UniversalClient
	keyPrefix  string
	defLimit   int
	rules      map[string]int
	leaseTTL   time.Duration
	adaptive   bool
	threshold  float64
	maxPenalty time.Duration
	penalty    *localDomainThrottle
}

func newRedisDomainThrottle(cfg redisDomainThrottleConfig) (domainThrottle, error) {
	addrs := normalizeRedisAddrs(cfg.Addrs)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("redis domain throttle requires at least one address")
	}
	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = defaultRedisDomainThrottlePrefix
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 58 * time.Second
	}

	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    addrs,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis domain throttle: %w", err)
	}

	return &redisDomainThrottle{
		client:     client,
		keyPrefix:  keyPrefix,
		defLimit:   cfg.DefLimit,
		rules:      cfg.Rules,
		leaseTTL:   cfg.LeaseTTL,
		adaptive:   cfg.Adaptive,
		threshold:  cfg.Threshold,
		maxPenalty: cfg.MaxPenalty,
		penalty:    newLocalDomainThrottle(cfg.DefLimit, cfg.Rules, cfg.Adaptive, cfg.Threshold, cfg.MaxPenalty),
	}, nil
}

func (d *redisDomainThrottle) acquire(domain string) domainThrottleLease {
	started := time.Now()
	domain = strings.ToLower(strings.TrimSpace(domain))
	backendError := false
	if p := d.currentPenalty(domain); p > 0 {
		time.Sleep(p)
	}

	limit := d.defLimit
	if v, ok := d.rules[domain]; ok && v > 0 {
		limit = v
	}
	if limit <= 0 {
		limit = 1
	}

	for {
		token, err := d.tryAcquire(domain, limit)
		if err != nil {
			backendError = true
			slog.Warn("domain throttle backend error", "component", "worker", "backend", "redis", "domain", domain, "error", err)
			return domainThrottleLease{
				release:      func() {},
				waitDuration: time.Since(started),
				backendError: backendError,
			}
		}
		if token != "" {
			return domainThrottleLease{
				release: func() {
					if err := d.release(domain, token); err != nil {
						slog.Warn("domain throttle release failed", "component", "worker", "backend", "redis", "domain", domain, "error", err)
					}
				},
				waitDuration: time.Since(started),
				backendError: backendError,
			}
		}
		time.Sleep(redisDomainThrottlePollInterval)
	}
}

func (d *redisDomainThrottle) observe(domain string, temporaryFailure bool) bool {
	d.penalty.observe(domain, temporaryFailure)
	if !d.adaptive {
		return false
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if err := d.observeRedis(domain, temporaryFailure); err != nil {
		slog.Warn("domain throttle observe failed", "component", "worker", "backend", "redis", "domain", domain, "error", err)
		return true
	}
	return false
}

func (d *redisDomainThrottle) tryAcquire(domain string, limit int) (string, error) {
	return d.tryAcquireContext(context.Background(), domain, limit, 0)
}

func (d *redisDomainThrottle) tryAcquireContext(parent context.Context, domain string, limit int, penalty time.Duration) (string, error) {
	nowMs := time.Now().UTC().UnixMilli()
	ttlMs := d.leaseTTL.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = 1
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := fmt.Sprintf("%x", random)

	ctx, cancel := context.WithTimeout(parent, redisDomainThrottleTimeout)
	defer cancel()
	result, err := redisDomainThrottleAcquireScript.Run(
		ctx,
		d.client,
		[]string{d.redisLeaseKey(domain)},
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(ttlMs, 10),
		strconv.Itoa(limit),
		token,
		strconv.FormatInt(penalty.Milliseconds(), 10),
	).Int()
	if err != nil {
		return "", err
	}
	if result == 1 {
		return token, nil
	}
	return "", nil
}

func (d *redisDomainThrottle) release(domain, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), redisDomainThrottleTimeout)
	defer cancel()
	return d.client.ZRem(ctx, d.redisLeaseKey(domain), token).Err()
}

func (d *redisDomainThrottle) currentPenalty(domain string) time.Duration {
	ctx, cancel := context.WithTimeout(context.Background(), redisDomainThrottleTimeout)
	defer cancel()

	value, err := d.client.HGet(ctx, d.redisStatsKey(domain), "penalty_ms").Result()
	if err == nil {
		ms, convErr := strconv.ParseInt(value, 10, 64)
		if convErr == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
		if convErr == nil {
			return 0
		}
	} else if err != redis.Nil {
		slog.Warn("domain throttle penalty lookup failed", "component", "worker", "backend", "redis", "domain", domain, "error", err)
	}

	st := d.penalty.get(domain)
	return d.penalty.currentPenalty(st)
}

func (d *redisDomainThrottle) observeRedis(domain string, temporaryFailure bool) error {
	temporaryFailureValue := 0
	if temporaryFailure {
		temporaryFailureValue = 1
	}
	maxPenaltyMs := d.maxPenalty.Milliseconds()
	if maxPenaltyMs <= 0 {
		maxPenaltyMs = 1
	}
	statsTTLms := redisDomainThrottleStatsTTL.Milliseconds()

	ctx, cancel := context.WithTimeout(context.Background(), redisDomainThrottleTimeout)
	defer cancel()
	_, err := redisDomainThrottleObserveScript.Run(
		ctx,
		d.client,
		[]string{d.redisStatsKey(domain)},
		strconv.Itoa(temporaryFailureValue),
		strconv.FormatFloat(d.threshold, 'f', -1, 64),
		strconv.FormatInt(maxPenaltyMs, 10),
		strconv.FormatInt(statsTTLms, 10),
	).Result()
	return err
}

func (d *redisDomainThrottle) redisLeaseKey(domain string) string {
	return d.keyPrefix + ":lease:" + hashDomainThrottleString(domain)
}

func (d *redisDomainThrottle) redisStatsKey(domain string) string {
	return d.keyPrefix + ":stats:" + hashDomainThrottleString(domain)
}

func normalizeRedisAddrs(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		out = append(out, addr)
	}
	return out
}

func hashDomainThrottleString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (d *redisDomainThrottle) tryLease(ctx context.Context, domain string) (domainThrottleLease, time.Duration) {
	if ctx.Err() != nil {
		return domainThrottleLease{}, time.Second
	}
	penalty := d.currentPenalty(domain)
	limit := d.defLimit
	if v := d.rules[domain]; v > 0 {
		limit = v
	}
	if limit <= 0 {
		limit = 1
	}
	token, err := d.tryAcquireContext(ctx, domain, limit, penalty)
	if err != nil {
		return domainThrottleLease{backendError: true}, time.Second
	}
	if token == "" {
		return domainThrottleLease{}, 100 * time.Millisecond
	}
	return domainThrottleLease{release: func() { _ = d.release(domain, token) }}, 0
}
