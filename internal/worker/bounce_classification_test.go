package worker

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func TestWorkerSuppressesOnlyConfirmedInvalidRecipients(t *testing.T) {
	tests := []struct {
		name, stage, response, category, rule string
		suppress, retry                       bool
	}{
		{"invalid recipient", "RCPT", "550 5.1.1 no mailbox", "invalid_recipient", "rcpt_5_1_1", true, false},
		{"data policy", "FINAL", "550 5.7.1 policy rejected", "policy", "enhanced_policy", false, false},
		{"sender error", "MAIL", "550 5.1.1 no sender", "unknown", "unknown", false, false},
		{"full mailbox", "RCPT", "552 5.2.2 full", "mailbox_full", "enhanced_mailbox_full", false, false},
		{"unknown permanent", "RCPT", "550 unavailable", "unknown", "unknown", false, false},
		{"temporary full", "RCPT", "452 4.2.2 full", "mailbox_full", "enhanced_mailbox_full", false, true},
		{"multiline invalid", "RCPT", "550-5.1.1 unknown\r\n550 5.1.1 mailbox", "invalid_recipient", "rcpt_5_1_1", true, false},
		{"auth required", "MAIL", "530 5.7.0 auth required", "auth", "smtp_auth", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var audit bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&audit, nil)))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			host, port := rejectingSMTPServer(t, tt.stage, tt.response)
			cfg := config.Config{DeliveryMode: "relay", RelayHost: host, RelayPort: port, Hostname: "mx.example.test", DialTimeout: time.Second, SendTimeout: time.Second, RetrySchedule: []time.Duration{time.Hour}}
			backend, err := queue.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			supPath := filepath.Join(t.TempDir(), "suppression.json")
			sup, err := bounce.NewSuppressionStore(supPath)
			if err != nil {
				t.Fatal(err)
			}
			// Existing entries must be retained through classification and persistence.
			if err := sup.Add("existing@example.test", "manual"); err != nil {
				t.Fatal(err)
			}
			msg := &model.Message{ID: "test-message", MailFrom: "sender@example.test", RcptTo: []string{"recipient@example.test"}, Data: []byte("Subject: test\r\n\r\nbody\r\n")}
			if err := backend.Enqueue(msg); err != nil {
				t.Fatal(err)
			}
			metrics := observability.NewMetrics()
			d := &Dispatcher{cfg: cfg, queue: backend, cl: delivery.NewClient(cfg), sup: sup, m: metrics}
			d.handleMessage(context.Background(), msg)
			restored, err := bounce.NewSuppressionStore(supPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := restored.IsSuppressed(msg.RcptTo[0]); got != tt.suppress {
				t.Fatalf("suppressed = %v, want %v", got, tt.suppress)
			}
			if !restored.IsSuppressed("existing@example.test") {
				t.Fatal("existing suppression lost")
			}
			state := "sent"
			if tt.retry {
				state = "retry"
			}
			stored, err := backend.ListState(state, 0)
			if err != nil || len(stored) != 1 {
				t.Fatalf("state %s = %d: %v", state, len(stored), err)
			}
			reports, err := backend.ListState("inbound", 0)
			if err != nil || len(reports) != 1 {
				t.Fatalf("DSN missing: %d %v", len(reports), err)
			}
			if reports[0].MailFrom != "" || !strings.Contains(string(reports[0].Data), "multipart/report") {
				t.Fatal("DSN compatibility changed")
			}
			logged := audit.String()
			if !strings.Contains(logged, `"event":"delivery_failure_classified"`) || !strings.Contains(logged, `"rule":"`+tt.rule+`"`) || !strings.Contains(logged, `"category":"`+tt.category+`"`) {
				t.Fatalf("missing classification audit: %s", logged)
			}
			if strings.Contains(logged, "recipient@example.test") {
				t.Fatal("audit leaked unmasked recipient")
			}
			snap := metrics.Snapshot()
			if snap["worker_bounce_class_"+tt.category+"_total"] != 1 || snap["worker_bounce_rule_"+tt.rule+"_total"] != 1 {
				t.Fatalf("classification metrics = %v", snap)
			}
		})
	}
}

func rejectingSMTPServer(t *testing.T, stage, response string) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 ready\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			verb := strings.Fields(line)[0]
			if verb == stage {
				fmt.Fprint(conn, response+"\r\n")
				return
			}
			if verb == "DATA" {
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
				fmt.Fprint(conn, response+"\r\n")
				return
			}
			fmt.Fprint(conn, "250 ok\r\n")
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("SMTP fixture did not stop")
		}
	})
	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}
