package deliverypause

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tamago0224/kuroshio-mta/internal/config"
)

// One slot contains a bounded hash plus expiry index. Redis time is authoritative
// for lookup and capacity cleanup. Dry runs do not modify even expired entries.
var redisScript = redis.NewScript(`
local tm=redis.call('TIME');local now=tonumber(tm[1])*1000+math.floor(tonumber(tm[2])/1000)
local op=ARGV[1];local domain=ARGV[2];local dry=ARGV[3]=='1'
if op=='lookup' then
 local expiry=tonumber(redis.call('ZSCORE',KEYS[2],domain) or '0')
 if expiry<=now then return '' end
 local value=redis.call('HGET',KEYS[1],domain)
 if not value then return redis.error_reply('invalid delivery pause state') end
 return value
end
if op=='remove' then
 local expiry=tonumber(redis.call('ZSCORE',KEYS[2],domain) or '0')
 local exists=expiry>now and redis.call('HEXISTS',KEYS[1],domain)==1
 if not dry then redis.call('HDEL',KEYS[1],domain);redis.call('ZREM',KEYS[2],domain) end
 if exists then return 1 else return 0 end
end
if op=='put' then
 local expiry=tonumber(ARGV[5])
 if expiry<=now or expiry>now+tonumber(ARGV[6]) then return -2 end
 local count=redis.call('ZCOUNT',KEYS[2],'('..now,'+inf')
 local old=tonumber(redis.call('ZSCORE',KEYS[2],domain) or '0')
 if count>=1024 and old<=now then return -1 end
 if not dry then
  local expired=redis.call('ZRANGEBYSCORE',KEYS[2],'-inf',now)
  for _,d in ipairs(expired) do redis.call('HDEL',KEYS[1],d) end
  redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
  redis.call('HSET',KEYS[1],domain,ARGV[4]);redis.call('ZADD',KEYS[2],expiry,domain)
 end
 return 1
end
local domains=redis.call('ZRANGEBYSCORE',KEYS[2],'('..now,'+inf')
local entries={}
for _,d in ipairs(domains) do
 local e=redis.call('HGET',KEYS[1],d);if not e then return redis.error_reply('invalid delivery pause state') end;table.insert(entries,e)
end
return entries
`)

type redisStore struct {
	client redis.UniversalClient
	keys   []string
}

// New opens the configured state backend. Redis persistence must be configured
// by the operator; failures never fall back to independent local state.
func New(ctx context.Context, cfg config.Config) (Store, error) {
	if !cfg.DeliveryPause.Enabled {
		return nil, nil
	}
	if err := cfg.DeliveryPause.Validate(cfg.DomainThrottleRedisAddrs); err != nil {
		return nil, err
	}
	if cfg.DeliveryPause.Backend != "redis" {
		return NewLocal(filepath.Join(cfg.QueueDir, "delivery-pauses.json"))
	}
	client := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: cfg.DomainThrottleRedisAddrs, Username: cfg.DomainThrottleRedisUsername, Password: cfg.DomainThrottleRedisPassword, DB: cfg.DomainThrottleRedisDB})
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, errors.New("delivery pause Redis unavailable")
	}
	prefix := cfg.DomainThrottleRedisKeyPrefix
	if prefix == "" {
		prefix = "kuroshio:domainthrottle"
	}
	key := prefix + ":{delivery-pauses}"
	return &redisStore{client: client, keys: []string{key + ":entries", key + ":expiry"}}, nil
}

func (s *redisStore) run(ctx context.Context, args ...any) *redis.Cmd {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return redisScript.Run(ctx, s.client, s.keys, args...)
}

func (s *redisStore) Lookup(ctx context.Context, domain string) (Entry, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return Entry{}, err
	}
	data, err := s.run(ctx, "lookup", domain, "1").Text()
	if err != nil {
		return Entry{}, err
	}
	if data == "" {
		return Entry{}, nil
	}
	var e Entry
	if err := json.Unmarshal([]byte(data), &e); err != nil || e.Domain != domain || e.ExpiresAt.IsZero() {
		return Entry{}, errors.New("invalid delivery pause Redis state")
	}
	return e, nil
}

func (s *redisStore) List(ctx context.Context) ([]Entry, error) {
	values, err := s.run(ctx, "list", "", "1").StringSlice()
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, value := range values {
		var e Entry
		if err := json.Unmarshal([]byte(value), &e); err != nil {
			return nil, errors.New("invalid delivery pause Redis state")
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Domain < entries[j].Domain })
	return entries, nil
}

func (s *redisStore) Put(ctx context.Context, e Entry, dryRun bool) error {
	e, err := Validate(e, time.Now())
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	dry := "0"
	if dryRun {
		dry = "1"
	}
	result, err := s.run(ctx, "put", e.Domain, dry, string(data), e.ExpiresAt.UnixMilli(), MaxDuration.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result == -1 {
		return ErrCapacity
	}
	if result == -2 {
		return ErrInvalid
	}
	return nil
}

func (s *redisStore) Remove(ctx context.Context, domain string, dryRun bool) (bool, error) {
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return false, err
	}
	dry := "0"
	if dryRun {
		dry = "1"
	}
	n, err := s.run(ctx, "remove", domain, dry).Int()
	return n == 1, err
}

func (s *redisStore) Close() error { return s.client.Close() }
