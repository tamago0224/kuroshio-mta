package smtp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/policyhook"
)

func TestPolicyHookSMTPResponsesAndBuiltInChecks(t *testing.T) {
	original := evaluateAuthWithPolicy
	t.Cleanup(func() { evaluateAuthWithPolicy = original })
	for _, tc := range []struct {
		name, stage, reply, body string
		status                   int
		delay                    time.Duration
		hold                     bool
		authReject               bool
		want                     int
		queued                   int
	}{
		{name: "accept", stage: "data", reply: `{"version":"kuroshio.smtp-policy.v1","action":"accept"}`, want: 250, queued: 1},
		{name: "DATA quarantine", stage: "data", reply: `{"version":"kuroshio.smtp-policy.v1","action":"quarantine"}`, hold: true, want: 250, queued: 1},
		{name: "quarantine without opt-in", stage: "data", reply: `{"version":"kuroshio.smtp-policy.v1","action":"quarantine"}`, want: 451},
		{name: "RCPT quarantine fails closed", stage: "rcpt", reply: `{"version":"kuroshio.smtp-policy.v1","action":"quarantine"}`, hold: true, want: 451},
		{name: "RCPT reject", stage: "rcpt", reply: `{"version":"kuroshio.smtp-policy.v1","action":"reject"}`, want: 550},
		{name: "DATA reject", stage: "data", reply: `{"version":"kuroshio.smtp-policy.v1","action":"reject"}`, want: 554},
		{name: "DATA tempfail", stage: "data", reply: `{"version":"kuroshio.smtp-policy.v1","action":"tempfail"}`, want: 451},
		{name: "timeout", stage: "data", delay: 100 * time.Millisecond, want: 451},
		{name: "invalid JSON", stage: "data", reply: `{`, want: 451},
		{name: "oversized response", stage: "data", reply: strings.Repeat("x", 1025), want: 451},
		{name: "HTTP failure", stage: "data", status: 503, want: 451},
		{name: "auth policy rejection precedes hook", stage: "data", authReject: true, want: 550},
		{name: "8BITMIME rejection precedes hook", stage: "data", body: "caf\xc3\xa9\r\n", want: 554},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			evaluator := mailauth.ActionAccept
			if tc.authReject {
				evaluator = mailauth.ActionReject
			}
			evaluateAuthWithPolicy = func(net.IP, string, string, []byte, mailauth.SPFPolicy) mailauth.Result {
				return mailauth.Result{Action: evaluator}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req policyhook.Request
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.Stage != tc.stage {
					w.Write([]byte(`{"version":"kuroshio.smtp-policy.v1","action":"accept"}`))
					return
				}
				calls.Add(1)
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				w.Write([]byte(tc.reply))
			}))
			defer server.Close()
			timeout := time.Second
			if tc.delay > 0 {
				timeout = 50 * time.Millisecond
			}
			cfg := config.Config{Quarantine: config.QuarantineConfig{Enabled: tc.hold, Retention: time.Hour}, Hostname: "mx.example.test", MaxMessageBytes: 1024, PolicyHook: config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true, Timeout: timeout, MaxResponseBytes: 1024}}
			hook, err := policyhook.New(cfg.PolicyHook, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer hook.Close()
			q := &recordingQueue{}
			s := &Server{recipientPolicy: allowTestRecipients{}, cfg: cfg, queue: q, policyHook: hook}
			client, conn := net.Pipe()
			done := make(chan struct{})
			go func() { defer close(done); s.handleConn(conn) }()
			defer func() { client.Close(); conn.Close(); <-done }()
			client.SetDeadline(time.Now().Add(2 * time.Second))
			r := bufio.NewReader(client)
			w := bufio.NewWriter(client)
			readSMTPResponse(t, r)
			for _, line := range []string{"EHLO client.test", "MAIL FROM:<sender@example.test>"} {
				mustWriteSMTPLine(t, w, line)
				if _, code := readSMTPResponse(t, r); code != 250 {
					t.Fatalf("setup: %d", code)
				}
			}
			mustWriteSMTPLine(t, w, "RCPT TO:<user@example.test>")
			_, code := readSMTPResponse(t, r)
			if tc.stage == "rcpt" {
				if code != tc.want {
					t.Fatalf("RCPT=%d", code)
				}
				return
			}
			if code != 250 {
				t.Fatalf("RCPT=%d", code)
			}
			mustWriteSMTPLine(t, w, "DATA")
			if _, code := readSMTPResponse(t, r); code != 354 {
				t.Fatal(code)
			}
			body := tc.body
			if body == "" {
				body = "Subject: test\r\n\r\nprivate message\r\n"
			}
			w.WriteString(body + ".\r\n")
			w.Flush()
			_, code = readSMTPResponse(t, r)
			if code != tc.want {
				t.Fatalf("DATA=%d want=%d", code, tc.want)
			}
			if len(q.msgs) != tc.queued {
				t.Fatalf("queued %d", len(q.msgs))
			}
			if tc.queued > 0 && ((q.msgs[0].Quarantine != nil) != tc.hold) {
				t.Fatal("hook quarantine metadata mismatch")
			}
			if (tc.authReject || tc.body != "") && calls.Load() != 0 {
				t.Fatal("hook accept bypassed built-in rejection")
			}
			if tc.want >= 400 {
				mustWriteSMTPLine(t, w, "RCPT TO:<next@example.test>")
				if _, code := readSMTPResponse(t, r); code != 503 {
					t.Fatal("hook refusal did not reset DATA transaction")
				}
			}
		})
	}
}

type countingPolicyHook struct{ calls atomic.Int64 }

func (h *countingPolicyHook) Evaluate(context.Context, policyhook.Request) (policyhook.Action, error) {
	h.calls.Add(1)
	return policyhook.Accept, nil
}
func TestPolicyHookCannotBypassSubmissionAuthentication(t *testing.T) {
	hook := &countingPolicyHook{}
	s := &Server{recipientPolicy: allowTestRecipients{}, cfg: config.Config{SubmissionAuth: true}, submission: true, policyHook: hook}
	var output strings.Builder
	w := bufio.NewWriter(&output)
	_, span := startSMTPCommandSpan(context.Background(), "test")
	s.handleMailCommand(context.Background(), &session{seenHelo: true}, nil, w, "FROM:<sender@example.test>", span)
	if !strings.HasPrefix(output.String(), "530 ") || hook.calls.Load() != 0 {
		t.Fatal("hook bypassed SMTP AUTH")
	}
}

func TestPolicyHookOverloadReturnsSMTP451(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte(`{"version":"kuroshio.smtp-policy.v1","action":"accept"}`))
	}))
	defer server.Close()
	cfg := config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true, MaxConcurrent: 1, Timeout: time.Second}
	client, err := policyhook.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan struct{})
	go func() { defer close(done); client.Evaluate(context.Background(), policyhook.Request{Stage: "rcpt"}) }()
	<-entered
	s := &Server{recipientPolicy: allowTestRecipients{}, policyHook: client}
	var output strings.Builder
	w := bufio.NewWriter(&output)
	_, span := startSMTPCommandSpan(context.Background(), "test")
	defer span.end()
	if s.applyPolicyHook(context.Background(), &session{}, "rcpt", "user@example.test", w, span) || !strings.HasPrefix(output.String(), "451 ") {
		t.Fatal("overload did not become SMTP451")
	}
	close(release)
	<-done
}
