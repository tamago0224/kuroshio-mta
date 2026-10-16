package shaping

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tamago0224/kuroshio-mta/internal/config"
)

// All checks and debits run atomically on one Redis cluster hash slot. Server
// time avoids cross-node clock skew. Expiring unique leases recover on crashes.
var acquireScript = redis.NewScript(`
local tm=redis.call('TIME')
local now=tonumber(tm[1])*1000+math.floor(tonumber(tm[2])/1000)
local mr=tonumber(ARGV[1]);local mb=math.max(tonumber(ARGV[2]),1)
local cr=tonumber(ARGV[3]);local cb=math.max(tonumber(ARGV[4]),1)
local maxc=tonumber(ARGV[5]);local message=tonumber(ARGV[6])
local ttl=tonumber(ARGV[7]);local token=ARGV[8];local idle=tonumber(ARGV[9])
local last=tonumber(redis.call('HGET',KEYS[1],'last') or now)
local elapsed=math.max(0,now-last)
local mt=math.min(mb,tonumber(redis.call('HGET',KEYS[1],'mt') or mb)+elapsed*mr/60000)
local ct=math.min(cb,tonumber(redis.call('HGET',KEYS[1],'ct') or cb)+elapsed*cr/60000)
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
local wait=0
if message==1 and mr>0 and mt<1 then wait=math.max(wait,(1-mt)*60000/mr) end
if cr>0 and ct<1 then wait=math.max(wait,(1-ct)*60000/cr) end
if maxc>0 and redis.call('ZCARD',KEYS[2])>=maxc then wait=math.max(wait,100) end
if wait==0 then
 if message==1 and mr>0 then mt=mt-1 end
 if cr>0 then ct=ct-1 end
 redis.call('ZADD',KEYS[2],now+ttl,token)
 redis.call('PEXPIRE',KEYS[2],ttl)
end
redis.call('HSET',KEYS[1],'last',now,'mt',mt,'ct',ct)
redis.call('PEXPIRE',KEYS[1],idle)
return math.ceil(wait)
`)

type redisLimiter struct {
	client redis.UniversalClient
	prefix string
}

func newRedisLimiter(cfg config.Config) (*redisLimiter, error) {
	if len(cfg.DomainThrottleRedisAddrs) == 0 {
		return nil, fmt.Errorf("MX shaping Redis requires domain_throttle_redis_addrs")
	}
	client := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: cfg.DomainThrottleRedisAddrs, Username: cfg.DomainThrottleRedisUsername, Password: cfg.DomainThrottleRedisPassword, DB: cfg.DomainThrottleRedisDB})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("MX shaping Redis ping: %w", err)
	}
	prefix := cfg.DomainThrottleRedisKeyPrefix
	if prefix == "" {
		prefix = "kuroshio:domainthrottle"
	}
	return &redisLimiter{client: client, prefix: prefix + ":mx_shaping"}, nil
}
func (l *redisLimiter) close() error { return l.client.Close() }
func (l *redisLimiter) acquire(ctx context.Context, key string, limits config.ShapingLimits, message bool, ttl time.Duration) (func(), time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, 0, err
	}
	token := fmt.Sprintf("%x", random)
	slot := fmt.Sprintf("%s:{%x}", l.prefix, sha256.Sum256([]byte(key)))
	messageCost := 0
	if message {
		messageCost = 1
	}
	wait, err := acquireScript.Run(ctx, l.client, []string{slot + ":state", slot + ":leases"}, limits.MessagesPerMinute, limits.MessageBurst, limits.ConnectionsPerMinute, limits.ConnectionBurst, limits.MaxConnections, messageCost, ttl.Milliseconds(), token, idleTTL(limits, ttl).Milliseconds()).Int64()
	if err != nil {
		return nil, 0, err
	}
	if wait > 0 {
		return nil, time.Duration(wait) * time.Millisecond, nil
	}
	return sync.OnceFunc(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = l.client.ZRem(ctx, slot+":leases", token).Err()
	}), 0, nil
}
