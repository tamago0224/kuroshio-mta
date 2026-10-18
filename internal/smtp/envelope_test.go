package smtp

import (
	"bufio"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/ingress"
)

func TestEnvelopeCommandsPreserveTransactionOnRejection(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		code     int
		dataCode int
	}{
		{
			name: "new sender clears recipients", command: "MAIL FROM:<new@example.com>",
			code: 250, dataCode: 503,
		},
		{
			name: "invalid sender preserves recipients", command: "MAIL FROM:<new@example.com> SIZE=invalid",
			code: 501, dataCode: 354,
		},
		{
			name: "recipient parameters preserve accepted recipient", command: "RCPT TO:<other@example.net> NOTIFY=SUCCESS",
			code: 555, dataCode: 354,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{recipientPolicy: allowTestRecipients{}, cfg: config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024}}
			client, r, w, cleanup := openRawTestSession(t, s)
			defer cleanup()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			readSMTPResponse(t, r)
			for _, command := range []string{
				"EHLO client.example",
				"MAIL FROM:<alice@example.com>",
				"RCPT TO:<bob@example.net>",
			} {
				mustWriteSMTPLine(t, w, command)
				if _, code := readSMTPResponse(t, r); code != 250 {
					t.Fatalf("%s: code = %d, want 250", command, code)
				}
			}
			mustWriteSMTPLine(t, w, tt.command)
			if _, code := readSMTPResponse(t, r); code != tt.code {
				t.Fatalf("envelope code = %d, want %d", code, tt.code)
			}
			mustWriteSMTPLine(t, w, "DATA")
			if _, code := readSMTPResponse(t, r); code != tt.dataCode {
				t.Fatalf("DATA code = %d, want %d", code, tt.dataCode)
			}
		})
	}
}

type envelopePeerConn struct {
	net.Conn
}

func (c envelopePeerConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 12345}
}

func TestMailFromRateLimitClosesConnection(t *testing.T) {
	rules, err := ingress.ParseRateRules("mailfrom:ip:1:1h")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{recipientPolicy: allowTestRecipients{},
		cfg:         config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024},
		flexLimiter: ingress.NewFlexibleLimiter(rules),
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go s.handleConn(envelopePeerConn{Conn: server})
	r, w := bufio.NewReader(client), bufio.NewWriter(client)
	readSMTPResponse(t, r)
	for _, command := range []string{"EHLO client.example", "MAIL FROM:<alice@example.com>"} {
		mustWriteSMTPLine(t, w, command)
		if _, code := readSMTPResponse(t, r); code != 250 {
			t.Fatalf("%s: code = %d, want 250", command, code)
		}
	}
	mustWriteSMTPLine(t, w, "MAIL FROM:<alice@example.com>")
	if _, code := readSMTPResponse(t, r); code != 421 {
		t.Fatalf("rate limit code = %d, want 421", code)
	}
	if _, err := r.ReadString('\n'); !errors.Is(err, io.EOF) {
		t.Fatalf("read after rate limit = %v, want EOF", err)
	}
}
