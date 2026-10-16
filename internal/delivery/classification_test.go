package delivery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name     string
		stage    SMTPStage
		code     int
		line     string
		category FailureCategory
		suppress bool
	}{
		{"invalid rcpt", StageRCPT, 550, "550 5.1.1 no such mailbox", FailureInvalidRecipient, true},
		{"data policy", StageDATA, 550, "550 5.7.1 blocked", FailurePolicy, false},
		{"mail sender invalid", StageMAIL, 550, "550 5.1.1 sender unknown", FailureUnknown, false},
		{"data invalid address", StageDATA, 550, "550 5.1.1 rejected", FailureUnknown, false},
		{"legacy missing stage", "", 550, "550 5.1.1 unknown", FailureUnknown, false},
		{"temporary rcpt", StageRCPT, 450, "450 4.1.1 try later", FailureUnknown, false},
		{"inconsistent class", StageRCPT, 450, "450 5.1.1 unknown", FailureUnknown, false},
		{"mailbox full", StageRCPT, 552, "552 5.2.2 mailbox full", FailureMailboxFull, false},
		{"temporary full", StageRCPT, 452, "452 4.2.2 mailbox full", FailureMailboxFull, false},
		{"auth", StageAUTH, 535, "535 5.7.8 credentials invalid", FailureAuth, false},
		{"tls policy", StageTLS, 550, "dane authentication failed", FailurePolicy, false},
		{"tls misleading status", StageTLS, 550, "550 5.1.1 no mailbox", FailurePolicy, false},
		{"unknown 550", StageRCPT, 550, "550 mailbox unavailable", FailureUnknown, false},
		{"status in prose", StageRCPT, 550, "550 error reference 5.1.1", FailureUnknown, false},
		{"malformed status", StageRCPT, 550, "550 5.1.1.9 bad", FailureUnknown, false},
		{"multiline", StageRCPT, 550, "550-5.1.1 unknown\n550 5.1.1 mailbox", FailureInvalidRecipient, true},
		{"missing multiline status", StageRCPT, 550, "550-first line\n550 5.1.1 mailbox", FailureUnknown, false},
		{"inconsistent reply code", StageRCPT, 550, "450 5.1.1 unknown", FailureUnknown, false},
		{"conflicting multiline", StageRCPT, 550, "550-5.1.1 unknown\n550 5.7.1 policy", FailureUnknown, false},
		{"dns status", StageRCPT, 451, "451 4.4.3 directory server failure", FailureDNS, false},
		{"transport status", StageDATA, 451, "451 4.4.1 no answer", FailureTransport, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &SMTPResponseError{Stage: tt.stage, Code: tt.code, Line: tt.line}
			got := ClassifyFailure(fmt.Errorf("wrapped: %w", err))
			if got.Category != tt.category || got.Suppress != tt.suppress || got.Rule == "" {
				t.Fatalf("classification = %+v", got)
			}
			if err.Permanent() != (tt.code >= 500 && tt.code < 600) || err.Temporary() != (tt.code >= 400 && tt.code < 500) {
				t.Fatal("SMTP retry semantics changed")
			}
		})
	}
	if got := ClassifyFailure(&net.DNSError{Err: "not found", Name: "example.test"}); got.Category != FailureDNS || got.Suppress {
		t.Fatalf("DNS = %+v", got)
	}
	if got := ClassifyFailure(io.EOF); got.Category != FailureTransport || got.Suppress {
		t.Fatalf("EOF = %+v", got)
	}
	if got := ClassifyFailure(errors.New("unknown")); got.Category != FailureUnknown || got.Suppress {
		t.Fatalf("unknown = %+v", got)
	}
}

func TestMultilineRejectionConsumed(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("550-5.1.1 unknown\r\n550 5.1.1 no mailbox\r\n250 ok\r\n"))
	err := atStage(StageRCPT, expect2xx(r))
	got := ClassifyFailure(err)
	if !got.Suppress || got.EnhancedStatus != "5.1.1" {
		t.Fatalf("classification = %+v", got)
	}
	if err := expect2xx(r); err != nil {
		t.Fatalf("multiline response not consumed: %v", err)
	}
}

func TestRequiredTLSRejectionHasPolicyStage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 ready\r\n")
		if _, err := r.ReadString('\n'); err != nil {
			result <- err
			return
		}
		fmt.Fprint(conn, "250-hello\r\n250 STARTTLS\r\n")
		line, err := r.ReadString('\n')
		if err != nil {
			result <- err
			return
		}
		if line != "STARTTLS\r\n" {
			result <- fmt.Errorf("expected STARTTLS, got %q", line)
			return
		}
		fmt.Fprint(conn, "454 4.7.0 TLS unavailable\r\n")
		line, err = r.ReadString('\n')
		if line != "" || !errors.Is(err, io.EOF) {
			result <- fmt.Errorf("unexpected plaintext after required TLS rejection: %q %v", line, err)
			return
		}
		result <- nil
	}()
	addr := listener.Addr().(*net.TCPAddr)
	client := NewClient(config.Config{Hostname: "mx.example.test", DialTimeout: time.Second, SendTimeout: time.Second})
	err = client.deliverHost(context.Background(), "127.0.0.1", addr.Port, &model.Message{}, "recipient@example.test", true, nil)
	got := ClassifyFailure(err)
	if got.Category != FailurePolicy || got.Stage != StageTLS || got.Suppress {
		t.Fatalf("TLS classification = %+v", got)
	}
	var response *SMTPResponseError
	if !errors.As(err, &response) || !response.Temporary() {
		t.Fatalf("TLS retry semantics changed: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
