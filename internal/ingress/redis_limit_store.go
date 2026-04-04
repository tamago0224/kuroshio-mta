package ingress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisRateLimitPrefix = "kuroshio:ratelimit"
	redisRateLimitTimeout       = 2 * time.Second
)

var redisRateLimitScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = "(" .. tostring(now - tonumber(ARGV[2]))
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local ttl_ms = tonumber(ARGV[5])

redis.call("ZREMRANGEBYSCORE", key, "-inf", cutoff)
local count = redis.call("ZCARD", key)
if count >= limit then
  redis.call("PEXPIRE", key, ttl_ms)
  return 0
end

redis.call("ZADD", key, now, member)
redis.call("PEXPIRE", key, ttl_ms)
return 1
`)

type RateLimitStoreConfig struct {
	Backend        string
	RedisAddrs     []string
	RedisUsername  string
	RedisPassword  string
	RedisDB        int
	RedisKeyPrefix string
}

type RedisLimitStoreConfig struct {
	Addrs     []string
	Username  string
	Password  string
	DB        int
	KeyPrefix string
}

type redisLimitStore struct {
	client    redis.UniversalClient
	keyPrefix string
	seq       atomic.Uint64
}

func NewLimitStore(cfg RateLimitStoreConfig) (LimitStore, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Backend)) {
	case "", "memory":
		return NewLocalLimitStore(), nil
	case "redis":
		return NewRedisLimitStore(RedisLimitStoreConfig{
			Addrs:     cfg.RedisAddrs,
			Username:  cfg.RedisUsername,
			Password:  cfg.RedisPassword,
			DB:        cfg.RedisDB,
			KeyPrefix: cfg.RedisKeyPrefix,
		})
	default:
		return nil, fmt.Errorf("unsupported rate limit backend %q", cfg.Backend)
	}
}

func NewRedisLimitStore(cfg RedisLimitStoreConfig) (LimitStore, error) {
	addrs := normalizeRedisAddrs(cfg.Addrs)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("redis rate limit store requires at least one address")
	}
	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = defaultRedisRateLimitPrefix
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
		return nil, fmt.Errorf("ping redis rate limit store: %w", err)
	}

	return &redisLimitStore{
		client:    client,
		keyPrefix: keyPrefix,
	}, nil
}

func (r *redisLimitStore) Allow(namespace, key string, now time.Time, limit int, window time.Duration) (bool, error) {
	windowMs := window.Milliseconds()
	if windowMs <= 0 {
		windowMs = 1
	}
	nowMs := now.UTC().UnixMilli()
	member := fmt.Sprintf("%d-%d", nowMs, r.seq.Add(1))

	ctx, cancel := context.WithTimeout(context.Background(), redisRateLimitTimeout)
	defer cancel()

	result, err := redisRateLimitScript.Run(
		ctx,
		r.client,
		[]string{r.redisKey(namespace, key)},
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(windowMs, 10),
		strconv.Itoa(limit),
		member,
		strconv.FormatInt(windowMs, 10),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (r *redisLimitStore) redisKey(namespace, key string) string {
	return r.keyPrefix + ":" + hashString(namespace) + ":" + hashString(key)
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

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
