package smtp

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

func TestSubmissionPasswordRequiresTLS(t *testing.T) {
	backend, err := userauth.NewStatic("alice@example.com:s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"AUTH PLAIN", "AUTH PLAIN invalid", "AUTH LOGIN", "AUTH LOGIN invalid"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			s := &Server{cfg: config.Config{Hostname: "test"}, submission: true, authBackend: backend}
			client, server := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(5 * time.Second))
			go s.handleConn(server)
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			mustWriteSMTPLine(t, w, "EHLO client")
			resp, _ := readSMTPResponse(t, r)
			if strings.Contains(resp, "AUTH") {
				t.Fatalf("plaintext AUTH advertised: %s", resp)
			}
			mustWriteSMTPLine(t, w, command)
			if _, code := readSMTPResponse(t, r); code != 538 {
				t.Fatalf("code=%d, want 538 before credential parsing or challenge", code)
			}
			mustWriteSMTPLine(t, w, "NOOP")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatalf("NOOP code=%d", code)
			}
		})
	}
}

func TestSubmissionTLSAuthentication(t *testing.T) {
	original := evaluateAuthWithPolicy
	evaluateAuthWithPolicy = func(net.IP, string, string, []byte, mailauth.SPFPolicy) mailauth.Result {
		return mailauth.Result{Action: mailauth.ActionAccept}
	}
	defer func() { evaluateAuthWithPolicy = original }()
	for _, implicit := range []bool{false, true} {
		name := "STARTTLS"
		if implicit {
			name = "implicit TLS"
		}
		t.Run(name, func(t *testing.T) {
			cert, err := selfSignedCert()
			if err != nil {
				t.Fatal(err)
			}
			backend, err := userauth.NewStatic("alice@example.com:s3cr3t")
			if err != nil {
				t.Fatal(err)
			}
			q := &recordingQueue{}
			s := &Server{queue: q, cfg: config.Config{Hostname: "test", MaxMessageBytes: 1 << 20, SubmissionAuth: true, SubmissionSenderID: true}, submission: true, implicitTLS: implicit, authBackend: backend, tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}}
			client, server := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(5 * time.Second))
			go s.handleConn(server)
			if !implicit {
				r, w := bufio.NewReader(client), bufio.NewWriter(client)
				readSMTPResponse(t, r)
				mustWriteSMTPLine(t, w, "EHLO client")
				readSMTPResponse(t, r)
				mustWriteSMTPLine(t, w, "STARTTLS")
				if _, code := readSMTPResponse(t, r); code != 220 {
					t.Fatalf("STARTTLS code=%d", code)
				}
			}
			roots := x509.NewCertPool()
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			roots.AddCert(leaf)
			c := tls.Client(client, &tls.Config{RootCAs: roots, ServerName: "mx.example.test", MinVersion: tls.VersionTLS12})
			if err := c.Handshake(); err != nil {
				t.Fatal(err)
			}
			r, w := bufio.NewReader(c), bufio.NewWriter(c)
			if implicit {
				if _, code := readSMTPResponse(t, r); code != 220 {
					t.Fatalf("greeting=%d", code)
				}
			}
			mustWriteSMTPLine(t, w, "EHLO client")
			resp, _ := readSMTPResponse(t, r)
			if !strings.Contains(resp, "AUTH PLAIN LOGIN") || strings.Contains(resp, "STARTTLS") {
				t.Fatalf("TLS EHLO: %s", resp)
			}
			if implicit {
				mustWriteSMTPLine(t, w, "AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHMzY3IzdA==")
			} else {
				mustWriteSMTPLine(t, w, "AUTH LOGIN YWxpY2VAZXhhbXBsZS5jb20=")
				if _, code := readSMTPResponse(t, r); code != 334 {
					t.Fatalf("LOGIN challenge=%d", code)
				}
				mustWriteSMTPLine(t, w, "czNjcjN0")
			}
			if _, code := readSMTPResponse(t, r); code != 235 {
				t.Fatalf("AUTH code=%d", code)
			}
			mustWriteSMTPLine(t, w, "MAIL FROM:<alice@example.com>")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatalf("MAIL code=%d", code)
			}
			mustWriteSMTPLine(t, w, "RCPT TO:<receiver@example.net>")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatalf("RCPT code=%d", code)
			}
			mustWriteSMTPLine(t, w, "DATA")
			if _, code := readSMTPResponse(t, r); code != 354 {
				t.Fatalf("DATA code=%d", code)
			}
			mustWriteSMTPLine(t, w, "Subject: TLS test\r\n\r\nhello\r\n.")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatalf("enqueue code=%d", code)
			}
			mustWriteSMTPLine(t, w, "QUIT")
			readSMTPResponse(t, r)
			if len(q.msgs) != 1 || !strings.Contains(string(q.msgs[0].Data), "with ESMTPS id") {
				t.Fatal("missing TLS message")
			}

		})
	}
}

func TestImplicitTLSRejectsPlaintext(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{implicitTLS: true, submission: true, tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}}
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); s.handleConn(server) }()
	_, _ = client.Write([]byte("EHLO plaintext.example\r\n"))
	var buf [512]byte
	n, _ := client.Read(buf[:])
	if strings.Contains(string(buf[:n]), "220") || strings.Contains(string(buf[:n]), "250") {
		t.Fatal("SMTP continued after TLS failure")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TLS failure did not close connection")
	}
}
