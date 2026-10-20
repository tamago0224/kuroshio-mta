//go:build integration

package deliverypause

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func startTestRedis(t *testing.T) string {
	t.Helper()
	if addr := os.Getenv("MTA_TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
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

func TestRedisPauseSharedAcrossNodes(t *testing.T) {
	addr := startTestRedis(t)
	ctx := context.Background()
	cfg := config.Config{DeliveryPause: config.DeliveryPauseConfig{Enabled: true, Backend: "redis"}, DomainThrottleRedisAddrs: []string{addr}, DomainThrottleRedisKeyPrefix: "test"}
	first, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	entry := Entry{Domain: "a.test", Actor: "ops", Reason: "maintenance", ExpiresAt: time.Now().Add(time.Hour)}
	if err := first.Put(ctx, entry, true); err != nil {
		t.Fatal(err)
	}
	if got, err := second.Lookup(ctx, "a.test"); err != nil || got.Domain != "" {
		t.Fatal("dry run modified shared state")
	}
	if err := first.Put(ctx, entry, false); err != nil {
		t.Fatal(err)
	}
	if got, err := second.Lookup(ctx, "a.test"); err != nil || got.Domain != "a.test" {
		t.Fatalf("shared pause: %+v %v", got, err)
	}
	if list, err := second.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := second.Remove(ctx, "a.test", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := first.Lookup(ctx, "a.test"); got.Domain == "" {
		t.Fatal("dry resume modified state")
	}
	if _, err := second.Remove(ctx, "a.test", false); err != nil {
		t.Fatal(err)
	}
	if got, err := first.Lookup(ctx, "a.test"); err != nil || got.Domain != "" {
		t.Fatal("resume not shared")
	}
	entry.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	if err := first.Put(ctx, entry, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got, err := second.Lookup(ctx, "a.test"); err != nil || got.Domain != "" {
		t.Fatal("Redis expiry ignored")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.Lookup(canceled, "a.test"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
