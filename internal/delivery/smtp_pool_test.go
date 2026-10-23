package delivery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

func TestSMTPConnectionPoolReusesSession(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go serveReusableSMTP(conn)
		}
	}()

	metrics := observability.NewMetrics()
	cfg := config.Config{
		Hostname: "sender.test", DialTimeout: time.Second, SendTimeout: time.Second,
		SMTPConnectionPool: config.SMTPConnectionPoolConfig{Enabled: true, MaxConnections: 2, MaxConnectionsKey: 1, MaxIdle: 1, MaxIdleKey: 1, IdleTimeout: time.Minute, MaxLifetime: time.Minute, MaxDeliveries: 10},
	}
	client := NewClientWithMetrics(cfg, metrics)
	port := listener.Addr().(*net.TCPAddr).Port
	msg := &model.Message{MailFrom: "sender@test", Data: []byte("Subject: reuse\r\n\r\nbody")}
	for range 2 {
		if err := client.deliverHost(context.Background(), "127.0.0.1", port, msg, "user@test", false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
	if got := metrics.Snapshot()["smtp_pool_reused_total"]; got != 1 {
		t.Fatalf("reuse metric = %d, want 1", got)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	<-done
}

func TestSMTPConnectionPoolWaiterUsesReturnedSession(t *testing.T) {
	cfg := config.SMTPConnectionPoolConfig{Enabled: true, MaxConnections: 1, MaxConnectionsKey: 1, MaxIdle: 1, MaxIdleKey: 1, IdleTimeout: time.Minute, MaxLifetime: time.Minute, MaxDeliveries: 10}
	pool := newSMTPConnectionPool(cfg, nil)
	key := smtpPoolKey{host: "mx.test", port: 25}
	peer := make(chan net.Conn, 1)
	dial := func() (*smtpSession, error) {
		client, server := net.Pipe()
		peer <- server
		return &smtpSession{conn: client, r: bufio.NewReader(client), w: bufio.NewWriter(client), createdAt: time.Now()}, nil
	}
	first, err := pool.get(context.Background(), key, dial)
	if err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan *smtpSession, 1)
	go func() { second, _ := pool.get(context.Background(), key, dial); secondResult <- second }()
	select {
	case <-secondResult:
		t.Fatal("waiter returned before session release")
	case <-time.After(20 * time.Millisecond):
	}
	pool.put(first, true)
	select {
	case second := <-secondResult:
		if second != first {
			t.Fatal("waiter did not reuse returned session")
		}
		pool.put(second, false)
	case <-time.After(time.Second):
		t.Fatal("waiter did not wake after session release")
	}
	(<-peer).Close()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

func serveReusableSMTP(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	fmt.Fprint(w, "220 ready\r\n")
	w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			fmt.Fprint(w, "250 hello\r\n")
		case strings.HasPrefix(command, "MAIL"), strings.HasPrefix(command, "RCPT"), command == "RSET":
			fmt.Fprint(w, "250 ok\r\n")
		case command == "DATA":
			fmt.Fprint(w, "354 continue\r\n")
			w.Flush()
			for {
				data, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if data == ".\r\n" {
					break
				}
			}
			fmt.Fprint(w, "250 queued\r\n")
		case command == "QUIT":
			fmt.Fprint(w, "221 bye\r\n")
			w.Flush()
			return
		default:
			fmt.Fprint(w, "500 unknown\r\n")
		}
		w.Flush()
	}
}

func BenchmarkSMTPConnectionReuse(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				b.Fatal(err)
			}
			var connections atomic.Int64
			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					connections.Add(1)
					go serveReusableSMTP(conn)
				}
			}()
			cfg := config.Config{Hostname: "sender.test", DialTimeout: time.Second, SendTimeout: time.Second,
				SMTPConnectionPool: config.SMTPConnectionPoolConfig{Enabled: enabled, MaxConnections: 4, MaxConnectionsKey: 4, MaxIdle: 4, MaxIdleKey: 4, IdleTimeout: time.Minute, MaxLifetime: time.Minute, MaxDeliveries: 1000000}}
			client := NewClient(cfg)
			port := listener.Addr().(*net.TCPAddr).Port
			msg := &model.Message{MailFrom: "sender@test", Data: []byte("Subject: benchmark\r\n\r\nbody")}
			b.ResetTimer()
			for b.Loop() {
				if err := client.deliverHost(context.Background(), "127.0.0.1", port, msg, "user@test", false, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(connections.Load())/float64(b.N), "connections/op")
			_ = client.Close()
			_ = listener.Close()
		})
	}
}
