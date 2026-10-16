//go:build integration

package shaping

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func startTestRedis(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("MTA_TEST_REDIS_SERVER")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("redis-server")
		if err != nil {
			t.Skip("redis-server unavailable; set MTA_TEST_REDIS_SERVER")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	probe := redis.NewClient(&redis.Options{Addr: addr})
	defer probe.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := probe.Ping(ctx).Err()
		cancel()
		if err == nil {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Redis did not start")
	return ""
}

func TestRedisSharedLimitsAcrossNodes(t *testing.T) {
	addr := startTestRedis(t)
	cfg := config.Config{DomainThrottleRedisAddrs: []string{addr}, DomainThrottleRedisKeyPrefix: "test"}
	first, err := newRedisLimiter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := newRedisLimiter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	nodes := []*redisLimiter{first, second}
	var count atomic.Int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	var releases []func()
	limits := config.ShapingLimits{MaxConnections: 2}
	for i := range 30 {
		wg.Go(func() {
			release, delay, err := nodes[i%2].acquire(context.Background(), "shared", limits, true, time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			if delay == 0 {
				count.Add(1)
				mu.Lock()
				releases = append(releases, release)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if count.Load() != 2 {
		t.Fatalf("concurrent shared admission %d", count.Load())
	}
	release, delay, err := first.acquire(context.Background(), "independent", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("other group blocked %s %v", delay, err)
	}
	release()
	for _, release := range releases {
		release()
		release()
	}
	release, delay, err = second.acquire(context.Background(), "shared", limits, true, time.Minute)
	if err != nil || delay != 0 {
		t.Fatalf("released leases unavailable %s %v", delay, err)
	}
	release()
	t.Run("rate and burst", func(t *testing.T) {
		limits := config.ShapingLimits{MessagesPerMinute: 1, MessageBurst: 2}
		for i := range 2 {
			release, delay, err := nodes[i].acquire(context.Background(), "rate", limits, true, time.Minute)
			if err != nil || delay != 0 {
				t.Fatalf("burst %d: %s %v", i, delay, err)
			}
			release()
		}
		_, delay, err := first.acquire(context.Background(), "rate", limits, true, time.Minute)
		if err != nil || delay <= 0 {
			t.Fatalf("shared rate ignored %s %v", delay, err)
		}
	})
	t.Run("connection-only debit", func(t *testing.T) {
		limits := config.ShapingLimits{ConnectionsPerMinute: 1, ConnectionBurst: 1}
		release, delay, err := first.acquire(context.Background(), "connection", limits, false, time.Minute)
		if err != nil || delay != 0 {
			t.Fatal(err)
		}
		release()
		_, delay, err = second.acquire(context.Background(), "connection", limits, false, time.Minute)
		if err != nil || delay <= 0 {
			t.Fatalf("connection rate ignored %s %v", delay, err)
		}
	})
	t.Run("crash lease expiry", func(t *testing.T) {
		limits := config.ShapingLimits{MaxConnections: 1}
		stale, delay, err := first.acquire(context.Background(), "crash", limits, true, 50*time.Millisecond)
		if err != nil || delay != 0 {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Millisecond)
		fresh, delay, err := second.acquire(context.Background(), "crash", limits, true, time.Minute)
		if err != nil || delay != 0 {
			t.Fatalf("lease did not expire %s %v", delay, err)
		}
		stale()
		_, delay, err = first.acquire(context.Background(), "crash", limits, true, time.Minute)
		if err != nil || delay == 0 {
			t.Fatalf("stale release removed fresh lease %s %v", delay, err)
		}
		fresh()
	})
	t.Run("fail closed", func(t *testing.T) {
		s := &Shaper{limiter: first, cfg: config.MXShapingConfig{RetryDelay: time.Second}, leaseTTL: time.Minute}
		first.close()
		_, err := s.Acquire(context.Background(), "closed", config.ShapingLimits{}, true)
		if _, ok := err.(*DeferredError); !ok {
			t.Fatal(fmt.Sprintf("backend failure not deferred: %v", err))
		}
	})
}
