package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

type testBearerBackend struct{}

func (testBearerBackend) AuthenticateBearerToken(ctx context.Context, user, token string) userauth.AuthResult {
	return userauth.AuthResult{Success: user == "alice@example.com" && (token == "xoauth-secret-valid-123" || token == strings.Repeat("xoauth-secret-valid-123", 50)), Principal: userauth.Principal{RequireExplicitSenderScope: true, Username: user, AuthSource: "test_bearer", AllowedSenderAddresses: []string{user}}, FailureReason: "invalid_token"}
}
func xoauthPayload(user, token string) string {
	return base64.StdEncoding.EncodeToString([]byte("user=" + user + "\x01auth=Bearer " + token + "\x01\x01"))
}

func TestXOAuth2Payload(t *testing.T) {
	if user, token, err := decodeXOAuth2(xoauthPayload("alice@example.com", "xoauth-secret-valid-123")); err != nil || user != "alice@example.com" || token != "xoauth-secret-valid-123" {
		t.Fatal("valid response rejected")
	}
	for _, payload := range []string{"*", "invalid", base64.StdEncoding.EncodeToString([]byte("user=alice\x01auth=Bearer token\x01")), xoauthPayload("alice", ""), xoauthPayload("alice", "bad token"), xoauthPayload("alice\x00", "token"), strings.Repeat("A", maxAuthLineBytes+1)} {
		if _, _, err := decodeXOAuth2(payload); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
}

func TestSubmissionXOAuth2TLS(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "initial", false: "challenge"}[initial], func(t *testing.T) {
			exporter := setupSMTPTraceExporter(t)
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previousLogger)
			cert, err := selfSignedCert()
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{cfg: config.Config{Hostname: "sub.example.test", SubmissionAuth: true, SubmissionSenderID: true}, submission: true, authBackend: &userauth.CombinedBackend{Bearer: testBearerBackend{}}, tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}}
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			_ = client.SetDeadline(time.Now().Add(10 * time.Second))
			go s.handleConn(server)
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			mustWriteSMTPLine(t, w, "EHLO client.example")
			resp, _ := readSMTPResponse(t, r)
			if strings.Contains(resp, "AUTH") {
				t.Fatal("bearer advertised before TLS")
			}
			mustWriteSMTPLine(t, w, "AUTH XOAUTH2 "+xoauthPayload("alice@example.com", "xoauth-secret-valid-123"))
			if _, code := readSMTPResponse(t, r); code != 538 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "STARTTLS")
			if _, code := readSMTPResponse(t, r); code != 220 {
				t.Fatal(code)
			}
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(leaf)
			encrypted := tls.Client(client, &tls.Config{RootCAs: roots, ServerName: leaf.DNSNames[0], MinVersion: tls.VersionTLS12})
			if err := encrypted.Handshake(); err != nil {
				t.Fatal(err)
			}
			r, w = bufio.NewReader(encrypted), bufio.NewWriter(encrypted)
			mustWriteSMTPLine(t, w, "EHLO client.example")
			resp, _ = readSMTPResponse(t, r)
			if !strings.Contains(resp, "AUTH XOAUTH2") || strings.Contains(resp, "PLAIN") {
				t.Fatal(resp)
			}
			mustWriteSMTPLine(t, w, "AUTH XOAUTH2 malformed-secret-response")
			if _, code := readSMTPResponse(t, r); code != 501 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "AUTH malformed-secret-mechanism")
			if _, code := readSMTPResponse(t, r); code != 501 {
				t.Fatal(code)
			}
			// A failed token completes the XOAUTH2 error challenge before a retry.
			mustWriteSMTPLine(t, w, "AUTH XOAUTH2 "+xoauthPayload("alice@example.com", "xoauth-secret-invalid-123"))
			if _, code := readSMTPResponse(t, r); code != 334 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "")
			if _, code := readSMTPResponse(t, r); code != 535 {
				t.Fatal(code)
			}
			if initial {
				mustWriteSMTPLine(t, w, "AUTH XOAUTH2 "+xoauthPayload("alice@example.com", strings.Repeat("xoauth-secret-valid-123", 50)))
			} else {
				mustWriteSMTPLine(t, w, "AUTH XOAUTH2")
				if _, code := readSMTPResponse(t, r); code != 334 {
					t.Fatal(code)
				}
				mustWriteSMTPLine(t, w, xoauthPayload("alice@example.com", "xoauth-secret-valid-123"))
			}
			if _, code := readSMTPResponse(t, r); code != 235 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "MAIL FROM:<other@example.com>")
			if _, code := readSMTPResponse(t, r); code != 553 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "MAIL FROM:<alice@example.com>")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "QUIT")
			readSMTPResponse(t, r)
			spans := waitForSpans(t, exporter, "smtp.session", "smtp.auth")
			recorded := fmt.Sprint(spans) + logs.String()
			for _, secret := range []string{"malformed-secret-response", "malformed-secret-mechanism", "xoauth-secret-valid-123", "xoauth-secret-invalid-123", xoauthPayload("alice@example.com", "xoauth-secret-valid-123")} {
				if strings.Contains(recorded, secret) {
					t.Fatal("credential leaked to logs or spans")
				}
			}
			if !strings.Contains(logs.String(), "submission auth succeeded") || !strings.Contains(logs.String(), "submission auth failed") {
				t.Fatal("missing auth audit events")
			}
		})
	}
}

func TestXOAuth2ErrorResponses(t *testing.T) {
	s := &Server{submission: true, authBackend: &userauth.CombinedBackend{Bearer: testBearerBackend{}}}
	for _, response := range []string{"*\r\n", "bad-response\r\n", strings.Repeat("A", maxAuthLineBytes+1) + "\r\n"} {
		var out strings.Builder
		result, err := s.handleAuth(context.Background(), bufio.NewReader(strings.NewReader(response)), bufio.NewWriter(&out), "XOAUTH2 "+xoauthPayload("alice@example.com", "xoauth-secret-invalid-123"))
		if response == "*\r\n" {
			if err != nil || result.Success {
				t.Fatal("cancellation failed")
			}
		} else if err == nil {
			t.Fatal("malformed challenge accepted")
		}
	}
}
