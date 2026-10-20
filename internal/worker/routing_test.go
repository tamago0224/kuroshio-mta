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
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func TestPartialRouteFailureRetriesOnlyUndeliveredRecipient(t *testing.T) {
	successHost, successPort, successCount := acceptingSMTPServer(t)
	failureHost, failurePort, failureCount := temporaryFailureSMTPServer(t)
	cfg := config.Config{
		DeliveryMode:  "mx",
		DialTimeout:   time.Second,
		SendTimeout:   time.Second,
		RetrySchedule: []time.Duration{time.Millisecond},
		DeliveryRoutes: []config.DeliveryRoute{
			{Name: "success", Domains: []string{"success.test"}, Mode: "relay", RelayHost: successHost, RelayPort: successPort},
			{Name: "temporary", Domains: []string{"retry.test"}, Mode: "relay", RelayHost: failureHost, RelayPort: failurePort},
		},
	}
	backend, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := delivery.NewClient(cfg)
	defer client.Close()
	dispatcher := &Dispatcher{cfg: cfg, queue: backend, cl: client}
	msg := &model.Message{
		ID:       "partial-route",
		MailFrom: "",
		RcptTo:   []string{"ok@success.test", "later@retry.test"},
		Data:     []byte("From: sender@example.test\r\n\r\nhello"),
	}
	if err := backend.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	dispatcher.handleMessage(context.Background(), msg)

	retry, err := backend.ListState("retry", 0)
	if err != nil || len(retry) != 1 {
		t.Fatalf("retry state = %+v, %v", retry, err)
	}
	if got := retry[0].RcptTo; len(got) != 1 || got[0] != "later@retry.test" {
		t.Fatalf("retry recipients = %v", got)
	}
	dispatcher.handleMessage(context.Background(), retry[0])
	if successCount.Load() != 1 {
		t.Fatalf("delivered recipient was sent %d times", successCount.Load())
	}
	if failureCount.Load() != 2 {
		t.Fatalf("temporary recipient attempts = %d", failureCount.Load())
	}
}

func temporaryFailureSMTPServer(t *testing.T) (string, int, *atomic.Int64) {
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
					if fields[0] == "RCPT" {
						fmt.Fprint(conn, "451 4.3.0 try later\r\n")
						return
					}
					fmt.Fprint(conn, "250 ok\r\n")
				}
			})
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done; wg.Wait() })
	return "127.0.0.1", listener.Addr().(*net.TCPAddr).Port, &count
}
