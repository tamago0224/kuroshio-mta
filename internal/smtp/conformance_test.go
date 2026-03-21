package smtp

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
)

func TestSMTPConformance(t *testing.T) {
	t.Run("RFC5321-4.1.4-MAIL-before-HELO-must-fail-503", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, bannerCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.2.1", "banner", bannerCode, 220)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.4", "MAIL before HELO/EHLO", code, 503)
	})

	t.Run("RFC5321-4.1.1.1-EHLO-without-domain-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.1", "EHLO without domain", code, 501)
	})

	t.Run("RFC5321-4.1.1.1-HELO-without-domain-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "HELO")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.1", "HELO without domain", code, 501)
	})

	t.Run("RFC5321-4.1.4-MAIL-parameters-after-HELO-must-fail-555", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "HELO client.example")
		_, heloCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.1", "HELO", heloCode, 250)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> SIZE=123")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.4", "MAIL parameters after HELO", code, 555)
	})

	t.Run("RFC5321-4.1.1.2-invalid-SIZE-parameter-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> SIZE=abc")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "invalid SIZE parameter", code, 501)
	})

	t.Run("RFC5321-4.1.1.2-invalid-BODY-parameter-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> BODY=BINARYMIME")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "invalid BODY parameter", code, 501)
	})

	t.Run("RFC5321-4.1.1.2-unsupported-MAIL-parameter-must-fail-555", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> RET=FULL")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "unsupported MAIL parameter", code, 555)
	})

	t.Run("RFC5321-4.1.1.2-invalid-MAIL-syntax-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL TO:<alice@invalid.invalid>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "invalid MAIL syntax", code, 501)
	})

	t.Run("RFC5321-4.1.1.2-missing-MAIL-path-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL FROM:")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "missing MAIL path", code, 501)
	})

	t.Run("RFC5321-4.1.1.2-unclosed-MAIL-path-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.2", "unclosed MAIL path", code, 501)
	})

	t.Run("RFC5321-4.1.1.5-RSET-must-clear-transaction", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "RSET")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "DATA")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.5", "DATA after RSET", code, 503)
	})

	t.Run("RFC5321-4.1.1.5-RSET-must-allow-new-mail-transaction", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "RSET")
		_, resetCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.5", "RSET", resetCode, 250)

		mustWriteSMTPLine(t, w, "MAIL FROM:<carol@invalid.invalid>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.5", "MAIL after RSET", code, 250)
	})

	t.Run("RFC5321-4.1.1.3-RCPT-before-MAIL-must-fail-503", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "RCPT before MAIL", code, 503)
	})

	t.Run("RFC5321-4.1.1.3-invalid-RCPT-syntax-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "RCPT FROM:<bob@invalid.invalid>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "invalid RCPT syntax", code, 501)
	})

	t.Run("RFC5321-4.1.1.3-empty-RCPT-path-must-fail-501", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "RCPT TO:<>")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "empty RCPT path", code, 501)
	})

	t.Run("RFC5321-4.1.1.3-unsupported-RCPT-parameter-must-fail-555", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid> NOTIFY=SUCCESS")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "unsupported RCPT parameter", code, 555)
	})

	t.Run("RFC5321-4.1.1.3-postmaster-must-be-accepted", func(t *testing.T) {
		q := &recordingQueue{}
		s := &Server{
			cfg:   config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024 * 1024},
			queue: q,
		}
		r, w, cleanup := openTestSession(t, s)
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "RCPT TO:<postmaster>")
		_, rcptCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "RCPT TO:<postmaster>", rcptCode, 250)

		mustWriteSMTPLine(t, w, "DATA")
		_, dataCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.4", "DATA prompt", dataCode, 354)
		if _, err := w.WriteString("Subject: postmaster\r\n\r\nok\r\n.\r\n"); err != nil {
			t.Fatalf("write DATA payload: %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("flush DATA payload: %v", err)
		}
		_, queuedCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.3", "queue postmaster mail", queuedCode, 250)
		if len(q.msgs) != 1 {
			t.Fatalf("RFC 5321 4.1.1.3: queued=%d want=1", len(q.msgs))
		}
		if len(q.msgs[0].RcptTo) != 1 || q.msgs[0].RcptTo[0] != "postmaster@mx.example.test" {
			t.Fatalf("RFC 5321 4.1.1.3: rcpt_to=%v want=[postmaster@mx.example.test]", q.msgs[0].RcptTo)
		}
	})

	t.Run("RFC5321-4.1.1.4-DATA-before-RCPT-must-fail-503", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)

		mustWriteSMTPLine(t, w, "DATA")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.4", "DATA before RCPT", code, 503)
	})

	t.Run("RFC1870-6-SIZE-over-limit-must-fail-552", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> SIZE=4096")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 1870 6", "SIZE limit check", code, 552)
	})

	t.Run("RFC6152-2-BODY-8BITMIME-must-accept-8bit-data", func(t *testing.T) {
		q := &recordingQueue{}
		s := &Server{
			cfg:   config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024 * 1024},
			queue: q,
		}
		r, w, cleanup := openTestSession(t, s)
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid> BODY=8BITMIME")
		_, mailCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 6152 2", "MAIL BODY=8BITMIME", mailCode, 250)
		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "DATA")
		_, dataCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.4", "DATA prompt", dataCode, 354)

		payload := "From: alice@invalid.invalid\r\nTo: bob@invalid.invalid\r\nSubject: conformance\r\n\r\ncaf\xc3\xa9\r\n.\r\n"
		if _, err := w.WriteString(payload); err != nil {
			t.Fatalf("write DATA payload: %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("flush DATA payload: %v", err)
		}
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 6152 2", "accept 8-bit body", code, 250)
		if len(q.msgs) != 1 {
			t.Fatalf("RFC 6152 2: queued=%d want=1", len(q.msgs))
		}
	})

	t.Run("RFC5321-4.5.3.1.4-command-line-limit", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		tooLong := "NOOP " + strings.Repeat("a", 520)
		mustWriteSMTPLine(t, w, tooLong)
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.5.3.1.4", "command line length", code, 500)
	})

	t.Run("RFC5321-4.5.3.1.6-data-line-limit-must-fail-500", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024 * 1024}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "MAIL FROM:<alice@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "RCPT TO:<bob@invalid.invalid>")
		_, _ = readSMTPResponse(t, r)
		mustWriteSMTPLine(t, w, "DATA")
		_, dataCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.4", "DATA prompt", dataCode, 354)

		longLine := strings.Repeat("a", 999)
		if _, err := w.WriteString(longLine + "\r\n.\r\n"); err != nil {
			t.Fatalf("write DATA payload: %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("flush DATA payload: %v", err)
		}
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.5.3.1.6", "DATA line length", code, 500)
	})

	t.Run("RFC5321-4.1.1.8-HELP-must-return-214", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "HELP")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.8", "HELP", code, 214)
	})

	t.Run("RFC5321-3.5.2-VRFY-may-return-252", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "VRFY postmaster")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 3.5.2", "VRFY", code, 252)
	})

	t.Run("RFC5321-3.5.1-EXPN-may-return-502", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EXPN staff")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 3.5.1", "EXPN", code, 502)
	})

	t.Run("RFC5321-4.1.1.10-QUIT-must-return-221", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "QUIT")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.10", "QUIT", code, 221)
	})

	t.Run("RFC5321-4.1.1.9-NOOP-must-return-250", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "NOOP")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.9", "NOOP", code, 250)
	})

	t.Run("RFC5321-4.2.4-unrecognized-command-must-return-500", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "FROBULATE")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.2.4", "unrecognized command", code, 500)
	})

	t.Run("RFC5321-4.2.4-empty-command-must-return-500", func(t *testing.T) {
		r, w, cleanup := openTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.2.4", "empty command", code, 500)
	})

	t.Run("RFC5321-4.1.1.10-QUIT-must-close-connection", func(t *testing.T) {
		client, r, w, cleanup := openRawTestSession(t, &Server{cfg: config.Config{Hostname: "mx.example.test"}})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "QUIT")
		_, code := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.10", "QUIT response", code, 221)

		if err := client.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, err := r.ReadByte()
		if err == nil {
			t.Fatal("RFC 5321 4.1.1.10: expected connection close after QUIT")
		}
		if err != io.EOF {
			if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
				t.Fatalf("RFC 5321 4.1.1.10: unexpected read error after QUIT: %v", err)
			}
			t.Fatal("RFC 5321 4.1.1.10: timed out waiting for connection close after QUIT")
		}
	})

	t.Run("RFC3207-4.2-STARTTLS-must-reset-session-state", func(t *testing.T) {
		cert, err := selfSignedCert()
		if err != nil {
			t.Fatalf("create cert: %v", err)
		}
		client, r, w, cleanup := openRawTestSession(t, &Server{
			cfg:       config.Config{Hostname: "mx.example.test"},
			tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		})
		defer cleanup()

		_, _ = readSMTPResponse(t, r) // banner
		mustWriteSMTPLine(t, w, "EHLO client.example")
		_, ehloCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 5321 4.1.1.1", "EHLO before STARTTLS", ehloCode, 250)

		mustWriteSMTPLine(t, w, "STARTTLS")
		_, starttlsCode := readSMTPResponse(t, r)
		expectRFCCode(t, "RFC 3207 4.2", "STARTTLS", starttlsCode, 220)

		tlsClient := tls.Client(client, &tls.Config{InsecureSkipVerify: true})
		if err := tlsClient.Handshake(); err != nil {
			t.Fatalf("tls handshake: %v", err)
		}
		defer tlsClient.Close()
		rt := bufio.NewReader(tlsClient)
		wt := bufio.NewWriter(tlsClient)

		mustWriteSMTPLine(t, wt, "MAIL FROM:<alice@invalid.invalid>")
		_, code := readSMTPResponse(t, rt)
		expectRFCCode(t, "RFC 3207 4.2", "MAIL without EHLO after STARTTLS", code, 503)

		mustWriteSMTPLine(t, wt, "STARTTLS")
		_, code = readSMTPResponse(t, rt)
		expectRFCCode(t, "RFC 3207 4.2", "STARTTLS while already under TLS", code, 503)
	})
}

func openTestSession(t *testing.T, s *Server) (*bufio.Reader, *bufio.Writer, func()) {
	t.Helper()
	client, server := net.Pipe()
	go s.handleConn(server)
	return bufio.NewReader(client), bufio.NewWriter(client), func() {
		_ = client.Close()
		_ = server.Close()
	}
}

func openRawTestSession(t *testing.T, s *Server) (net.Conn, *bufio.Reader, *bufio.Writer, func()) {
	t.Helper()
	client, server := net.Pipe()
	go s.handleConn(server)
	return client, bufio.NewReader(client), bufio.NewWriter(client), func() {
		_ = client.Close()
		_ = server.Close()
	}
}

func expectRFCCode(t *testing.T, section, context string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: %s got=%d want=%d", section, context, got, want)
	}
}
