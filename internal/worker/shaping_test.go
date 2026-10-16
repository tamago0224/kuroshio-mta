package worker

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func TestShapingDefersWithoutAttemptsAndOtherGroupsProgress(t *testing.T) {
	host, port, count := acceptingSMTPServer(t)
	cfg := config.Config{DeliveryMode: "relay", RelayHost: host, RelayPort: port, DialTimeout: time.Second, SendTimeout: time.Second, MaxAttempts: 1, MXShaping: config.MXShapingConfig{Enabled: true, Providers: []config.ShapingProvider{
		{Name: "shared", Domains: []string{"a.test", "b.test"}, Limits: config.ShapingLimits{MessagesPerMinute: 1, MessageBurst: 1}},
		{Name: "independent", Domains: []string{"c.test"}, Limits: config.ShapingLimits{MessagesPerMinute: 1, MessageBurst: 1}},
	}}}
	backend, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metrics := observability.NewMetrics()
	client := delivery.NewClientWithMetrics(cfg, metrics)
	defer client.Close()
	d := &Dispatcher{cfg: cfg, queue: backend, cl: client, m: metrics}
	first := &model.Message{ID: "first", RcptTo: []string{"user@a.test"}, Data: []byte("Subject: test\r\n\r\nhello")}
	if err := backend.Enqueue(first); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(context.Background(), first)
	mixed := &model.Message{ID: "mixed", Attempts: 12, LastError: "prior failure", RcptTo: []string{"user@b.test", "user@c.test"}, Data: first.Data}
	if err := backend.Enqueue(mixed); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(context.Background(), mixed)
	if count.Load() != 2 {
		t.Fatalf("shared limiter bypass or unrelated group stalled: %d dials", count.Load())
	}
	retry, err := backend.ListState("retry", 0)
	if err != nil || len(retry) != 1 {
		t.Fatalf("retry %v %v", retry, err)
	}
	if retry[0].Attempts != 12 || len(retry[0].RcptTo) != 1 || retry[0].RcptTo[0] != "user@b.test" || retry[0].LastError != "prior failure" {
		t.Fatalf("attempt consumed or delivered recipient retained: %+v", retry[0])
	}
	inbound, err := backend.ListState("inbound", 0)
	if err != nil || len(inbound) != 0 {
		t.Fatal("shaping wait generated DSN")
	}
	if metrics.Snapshot()["worker_shaping_deferred_total"] != 1 || metrics.Snapshot()["mx_shaping_deferred_total"] != 1 {
		t.Fatal("deferral metrics missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := &model.Message{ID: "canceled", RcptTo: []string{"user@c.test"}, Data: first.Data}
	if err := backend.Enqueue(canceled); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(ctx, canceled)
	if canceled.Attempts != 0 || count.Load() != 2 {
		t.Fatal("canceled work consumed an attempt or dial")
	}
}

func TestShapingDomainLimitIsNonblocking(t *testing.T) {
	host, port, count := acceptingSMTPServer(t)
	cfg := config.Config{WorkerCount: 1, DeliveryMode: "relay", RelayHost: host, RelayPort: port, DialTimeout: time.Second, SendTimeout: time.Second, MXShaping: config.MXShapingConfig{Enabled: true}}
	backend, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	throttle := newLocalDomainThrottle(1, nil, false, 0.3, time.Second)
	held := throttle.acquire("a.test")
	defer held.release()
	client := delivery.NewClient(cfg)
	defer client.Close()
	d := &Dispatcher{cfg: cfg, queue: backend, cl: client, throttle: throttle}
	for i, domain := range []string{"a.test", "c.test"} {
		msg := &model.Message{ID: fmt.Sprint(i), RcptTo: []string{"user@" + domain}}
		if err := backend.Enqueue(msg); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- d.processBatch(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("domain throttle occupied worker")
	}
	if count.Load() != 1 {
		t.Fatalf("other group did not progress: %d", count.Load())
	}
}

func acceptingSMTPServer(t *testing.T) (string, int, *atomic.Int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			count.Add(1)
			wg.Go(func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				r := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 ready\r\n")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					fields := strings.Fields(line)
					if len(fields) == 0 {
						return
					}
					switch fields[0] {
					case "DATA":
						fmt.Fprint(conn, "354 send\r\n")
						for {
							line, err = r.ReadString('\n')
							if err != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
						}
						fmt.Fprint(conn, "250 queued\r\n")
					case "QUIT":
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "250 ok\r\n")
					}
				}
			})
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done; wg.Wait() })
	return "127.0.0.1", listener.Addr().(*net.TCPAddr).Port, &count
}

func TestNonblockingPenaltyEventuallyAllowsDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		throttle := newLocalDomainThrottle(2, nil, true, 0.3, time.Second)
		state := throttle.get("a.test")
		state.penalty = time.Second
		lease, wait := throttle.tryLease(context.Background(), "a.test")
		if wait != 0 {
			t.Fatal("first shaping domain lease unavailable")
		}
		lease.release()
		if _, wait := throttle.tryLease(context.Background(), "a.test"); wait <= 0 {
			t.Fatal("penalty not enforced")
		}
		time.Sleep(time.Second)
		lease, wait = throttle.tryLease(context.Background(), "a.test")
		if wait != 0 {
			t.Fatal("penalty caused permanent deferral")
		}
		lease.release()
	})
}
