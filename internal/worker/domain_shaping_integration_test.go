//go:build integration

package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisNonblockingDomainPenaltyAcrossNodes(t *testing.T) {
	binary := os.Getenv("MTA_TEST_REDIS_SERVER")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("redis-server")
		if err != nil {
			t.Skip("redis-server unavailable")
		}
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no", "--dir", dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	defer client.Close()
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := client.Ping(ctx).Err()
		cancel()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("Redis did not start")
	}
	makeNode := func() *redisDomainThrottle {
		return &redisDomainThrottle{client: client, keyPrefix: "test", defLimit: 2, leaseTTL: time.Minute, penalty: newLocalDomainThrottle(2, nil, true, 0.3, time.Second)}
	}
	first, second := makeNode(), makeNode()
	if err := client.HSet(context.Background(), first.redisStatsKey("a.test"), "penalty_ms", 250).Err(); err != nil {
		t.Fatal(err)
	}
	lease, wait := first.tryLease(context.Background(), "a.test")
	if wait != 0 || lease.backendError {
		t.Fatal("first lease unavailable")
	}
	lease.release()
	if _, wait := second.tryLease(context.Background(), "a.test"); wait <= 0 {
		t.Fatal("penalty not shared")
	}
	time.Sleep(300 * time.Millisecond)
	lease, wait = second.tryLease(context.Background(), "a.test")
	if wait != 0 || lease.backendError {
		t.Fatal("penalty did not recover")
	}
	lease.release()
	// With penalty disabled, independent nodes must have distinct lease tokens.
	if err := client.HSet(context.Background(), first.redisStatsKey("a.test"), "penalty_ms", 0).Err(); err != nil {
		t.Fatal(err)
	}
	a, wait := first.tryLease(context.Background(), "a.test")
	if wait != 0 {
		t.Fatal("first concurrent lease")
	}
	b, wait := second.tryLease(context.Background(), "a.test")
	if wait != 0 {
		t.Fatal("second concurrent lease")
	}
	if _, wait := first.tryLease(context.Background(), "a.test"); wait <= 0 {
		t.Fatal("cross-node leases collided")
	}
	a.release()
	b.release()
}
