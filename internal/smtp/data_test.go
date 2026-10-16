package smtp

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

type dataTestQueue struct {
	recordingQueue
	err error
}

func (q *dataTestQueue) Enqueue(msg *model.Message) error {
	if q.err != nil {
		return q.err
	}
	return q.recordingQueue.Enqueue(msg)
}

func TestDataOutcomesPreserveSessionBehavior(t *testing.T) {
	original := evaluateAuthWithPolicy
	t.Cleanup(func() { evaluateAuthWithPolicy = original })
	tests := []struct {
		name       string
		action     mailauth.Action
		body       string
		queueErr   error
		code       int
		rcptCode   int
		queued     uint64
		queueFails uint64
	}{
		{
			name: "accepted", action: mailauth.ActionAccept, body: "..leading\r\n",
			code: 250, rcptCode: 503, queued: 1,
		},
		{
			name: "quarantined", action: mailauth.ActionQuarantine, body: "..leading\r\n",
			code: 250, rcptCode: 503, queued: 1,
		},
		{
			name: "auth rejected", action: mailauth.ActionReject, body: "..leading\r\n",
			code: 550, rcptCode: 503,
		},
		{
			name: "8bit rejected", action: mailauth.ActionAccept, body: "caf\xc3\xa9\r\n",
			code: 554, rcptCode: 503,
		},
		{
			name: "queue capacity exhausted", action: mailauth.ActionAccept, body: "..leading\r\n",
			queueErr: queue.ErrCapacity, code: 451, rcptCode: 250, queueFails: 1,
		},
		{
			name: "enqueue failed", action: mailauth.ActionAccept, body: "..leading\r\n",
			queueErr: errors.New("queue unavailable"), code: 451, rcptCode: 250, queueFails: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The evaluator hook is package-wide, so these sessions run sequentially.
			evaluateAuthWithPolicy = func(_ net.IP, _, _ string, _ []byte, _ mailauth.SPFPolicy) mailauth.Result {
				return mailauth.Result{Action: tt.action}
			}
			q := &dataTestQueue{err: tt.queueErr}
			metrics := observability.NewMetrics()
			s := &Server{
				cfg:     config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024},
				queue:   q,
				metrics: metrics,
			}
			client, server := net.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleConn(server)
			}()
			defer func() {
				client.Close()
				server.Close()
				<-done
			}()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			for _, command := range []string{
				"EHLO client.example",
				"MAIL FROM:<alice@example.com> BODY=7BIT",
				"RCPT TO:<bob@example.net>",
			} {
				mustWriteSMTPLine(t, w, command)
				if _, code := readSMTPResponse(t, r); code != 250 {
					t.Fatalf("%s: code = %d, want 250", command, code)
				}
			}
			mustWriteSMTPLine(t, w, "DATA")
			if _, code := readSMTPResponse(t, r); code != 354 {
				t.Fatalf("DATA code = %d, want 354", code)
			}
			if _, err := w.WriteString("Subject: data\r\n\r\n" + tt.body + ".\r\n"); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if _, code := readSMTPResponse(t, r); code != tt.code {
				t.Fatalf("DATA result = %d, want %d", code, tt.code)
			}
			mustWriteSMTPLine(t, w, "RCPT TO:<next@example.net>")
			if _, code := readSMTPResponse(t, r); code != tt.rcptCode {
				t.Fatalf("next RCPT code = %d, want %d", code, tt.rcptCode)
			}
			mustWriteSMTPLine(t, w, "QUIT")
			readSMTPResponse(t, r)
			<-done
			if uint64(len(q.msgs)) != tt.queued {
				t.Fatalf("queued messages = %d, want %d", len(q.msgs), tt.queued)
			}
			if tt.queued > 0 {
				raw := string(q.msgs[0].Data)
				if !strings.HasSuffix(raw, "\r\n\r\n.leading\r\n") {
					t.Fatal("queued body did not preserve CRLF and remove dot-stuffing")
				}
				if !strings.HasPrefix(raw, "Received: ") || !strings.Contains(raw, "Authentication-Results: ") {
					t.Fatal("queued message is missing trace or authentication headers")
				}
				quarantine := strings.Contains(raw, "X-Kuroshio-Quarantine: true\r\n")
				if quarantine != (tt.action == mailauth.ActionQuarantine) {
					t.Fatalf("quarantine header = %v, action = %s", quarantine, tt.action)
				}
				if len(q.msgs[0].RcptTo) != 1 || q.msgs[0].RcptTo[0] != "bob@example.net" {
					t.Fatal("queued recipients changed after the transaction ended")
				}
			}
			snapshot := metrics.Snapshot()
			if snapshot["smtp_queued_messages_total"] != tt.queued || snapshot["smtp_enqueue_fail_total"] != tt.queueFails {
				t.Fatalf("unexpected queue metrics: %v", snapshot)
			}
		})
	}
}
