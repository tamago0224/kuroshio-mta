package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

func TestConnectionReservations(t *testing.T) {
	s := &Server{cfg: config.Config{SMTPLimits: config.SMTPLimitsConfig{MaxConnections: 2, MaxConnectionsPerIP: 1}}}
	if !s.acquireConnection("") || !s.acquireConnection("") || s.acquireConnection("") {
		t.Fatal("global cap not enforced")
	}
	s.releaseConnection("")
	if !s.acquireConnection("") {
		t.Fatal("slot not released")
	}
	if !s.acquireConnection("192.0.2.1") || s.acquireConnection("192.0.2.1") || !s.acquireConnection("192.0.2.2") {
		t.Fatal("IP cap not isolated")
	}
	s.releaseConnection("192.0.2.1")
	if !s.acquireConnection("192.0.2.1") {
		t.Fatal("IP slot not released")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				if s.acquireConnection("192.0.2.3") {
					s.releaseConnection("192.0.2.3")
				}
			}
		})
	}
	wg.Wait()
	if _, exists := s.connectionsPerIP["192.0.2.3"]; exists {
		t.Fatal("idle IP retained")
	}
}

func TestLimitedConnection(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		c := &limitedConn{Conn: server, expires: time.Now().Add(time.Second), idle: time.Second, remaining: 3}
		go func() { _, _ = io.WriteString(client, "abcde") }()
		b := make([]byte, 10)
		if n, err := c.Read(b); n != 3 || err != nil {
			t.Fatalf("read = %d, %v", n, err)
		}
		if _, err := c.Read(b); !errors.Is(err, errSessionBytes) {
			t.Fatalf("quota error = %v", err)
		}
	})
	for _, name := range []string{"idle", "lifetime"} {
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			c := &limitedConn{Conn: server, expires: time.Now().Add(time.Second), idle: 20 * time.Millisecond, remaining: 100}
			if name == "lifetime" {
				c.expires = time.Now().Add(20 * time.Millisecond)
				c.idle = time.Second
			}
			_, err := c.Read(make([]byte, 1))
			if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
				t.Fatalf("expected timeout, got %v", err)
			}
		})
	}
}

func TestBoundedDataLine(t *testing.T) {
	_, err := readData(bufio.NewReader(strings.NewReader(strings.Repeat("x", 100000))), 1000000)
	if !errors.Is(err, errDataLineTooLong) {
		t.Fatalf("unterminated line: %v", err)
	}
}

func TestSessionRecipientAndFailureLimits(t *testing.T) {
	s := &Server{recipientPolicy: allowTestRecipients{}, metrics: observability.NewMetrics(), cfg: config.Config{
		Hostname: "mx.example.test", SMTPLimits: config.SMTPLimitsConfig{MaxRecipients: 1, MaxRcptFailures: 2},
	}}
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); s.handleConn(server) }()
	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)
	for _, step := range []struct {
		command string
		code    int
	}{
		{"EHLO client.test", 250}, {"MAIL FROM:<sender@example.test>", 250},
		{"RCPT TO:<first@example.test>", 250}, {"RCPT TO:<second@example.test>", 452},
		{"RSET", 250}, {"MAIL FROM:<sender@example.test>", 250},
		{"RCPT TO:<first@example.test>", 250}, {"RCPT TO:<second@example.test>", 452},
	} {
		mustWriteSMTPLine(t, w, step.command)
		if _, code := readSMTPResponse(t, r); code != step.code {
			t.Fatalf("%s: %d, want %d", step.command, code, step.code)
		}
	}
	if _, code := readSMTPResponse(t, r); code != 421 {
		t.Fatalf("failure limit = %d", code)
	}
	<-done
	if len(s.connectionsPerIP) != 0 {
		t.Fatal("connection slot leaked")
	}
}

func TestSessionCancellationReleasesIP(t *testing.T) {
	s := &Server{cfg: config.Config{Hostname: "test"}}
	client, server := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.handleConnWithContext(ctx, server) }()
	_, _ = readSMTPResponse(t, bufio.NewReader(client))
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled session remained blocked")
	}
	if len(s.connectionsPerIP) != 0 {
		t.Fatal("IP slot leaked")
	}
}

func TestSessionAuthFailureLimitSurvivesReset(t *testing.T) {
	backend, err := userauth.NewStatic("alice@example.test:correct")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{submission: true, authBackend: backend, cfg: config.Config{
		Hostname: "mx.test", SubmissionAllowInsecureAuth: true,
		SMTPLimits: config.SMTPLimitsConfig{MaxAuthFailures: 2},
	}}
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); s.handleConn(server) }()
	r, w := bufio.NewReader(client), bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)
	command := "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00alice@example.test\x00wrong"))
	for _, step := range []struct {
		command string
		code    int
	}{
		{"EHLO client.test", 250}, {command, 535}, {"RSET", 250}, {"EHLO client.test", 250}, {command, 535},
	} {
		mustWriteSMTPLine(t, w, step.command)
		if _, code := readSMTPResponse(t, r); code != step.code {
			t.Fatalf("%s: %d, want %d", step.command, code, step.code)
		}
	}
	if _, code := readSMTPResponse(t, r); code != 421 {
		t.Fatalf("auth limit = %d", code)
	}
	<-done
}

func TestSessionMultipleMessagesAndLimit(t *testing.T) {
	original := evaluateAuthWithPolicy
	t.Cleanup(func() { evaluateAuthWithPolicy = original })
	evaluateAuthWithPolicy = func(net.IP, string, string, []byte, mailauth.SPFPolicy) mailauth.Result {
		return mailauth.Result{Action: mailauth.ActionAccept}
	}
	q := &recordingQueue{}
	s := &Server{recipientPolicy: allowTestRecipients{}, queue: q, cfg: config.Config{
		Hostname: "mx.test", MaxMessageBytes: 4096, SMTPLimits: config.SMTPLimitsConfig{MaxMessages: 2},
	}}
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); s.handleConn(server) }()
	r, w := bufio.NewReader(client), bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "EHLO client.test")
	_, _ = readSMTPResponse(t, r)
	for range 2 {
		for _, step := range []struct {
			command string
			code    int
		}{
			{"MAIL FROM:<sender@example.test>", 250}, {"RCPT TO:<user@example.test>", 250}, {"DATA", 354},
			{"Subject: test\r\n\r\nhello\r\n.", 250},
		} {
			mustWriteSMTPLine(t, w, step.command)
			if _, code := readSMTPResponse(t, r); code != step.code {
				t.Fatalf("%s: %d, want %d", step.command, code, step.code)
			}
		}
	}
	if _, code := readSMTPResponse(t, r); code != 421 {
		t.Fatalf("message limit = %d", code)
	}
	<-done
	if len(q.msgs) != 2 {
		t.Fatalf("queued %d messages", len(q.msgs))
	}
}

func TestTLSResetPreservesConnectionLimits(t *testing.T) {
	ss := &session{authFailures: 2, rcptFailures: 3, messages: 4}
	ss.resetAfterTLS()
	if ss.authFailures != 2 || ss.rcptFailures != 3 || ss.messages != 4 {
		t.Fatal("TLS reset erased connection limits")
	}
}
