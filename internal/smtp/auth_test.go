package smtp

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

// passwordOnlyBackend exposes the original Backend contract without DetailedBackend.
type passwordOnlyBackend struct {
	userauth.Backend
}

func TestPasswordAuthBackendCompatibility(t *testing.T) {
	backend, err := userauth.NewStatic("alice@example.com:s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	backends := []struct {
		name    string
		backend userauth.Backend
	}{
		{name: "detailed", backend: backend},
		{name: "password only", backend: passwordOnlyBackend{Backend: backend}},
	}
	for _, b := range backends {
		for _, mechanism := range []string{"PLAIN", "LOGIN"} {
			for _, password := range []string{"s3cr3t", "wrong"} {
				t.Run(b.name+"/"+mechanism+"/"+password, func(t *testing.T) {
					t.Parallel()
					encode := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
					command := "PLAIN " + encode("\x00alice@example.com\x00"+password)
					input := ""
					if mechanism == "LOGIN" {
						command = "LOGIN " + encode("alice@example.com")
						input = encode(password) + "\r\n"
					}
					s := &Server{submission: true, authBackend: b.backend}
					result, err := s.handleAuth(
						t.Context(),
						bufio.NewReader(strings.NewReader(input)),
						bufio.NewWriter(io.Discard),
						command,
					)
					if err != nil {
						t.Fatal(err)
					}
					want := backend.AuthenticatePasswordDetailed("alice@example.com", password)
					if b.name == "password only" {
						want.FailureReason = ""
					}
					if !reflect.DeepEqual(result, want) {
						t.Fatalf("result = %+v, want %+v", result, want)
					}
				})
			}
		}
	}
}

func TestAuthFramingErrorRequiresDisconnect(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "incomplete", input: "unfinished"},
		{name: "oversized", input: strings.Repeat("A", maxAuthLineBytes+1) + "\r\nNOOP\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{
				submission:  true,
				authBackend: &userauth.CombinedBackend{Bearer: testBearerBackend{}},
			}
			ss := &session{extended: true, tls: true}
			var output strings.Builder
			err := s.handleAuthCommand(
				t.Context(),
				context.Background(),
				ss,
				bufio.NewReader(strings.NewReader(tt.input)),
				bufio.NewWriter(&output),
				"XOAUTH2",
			)
			if !errors.Is(err, errSASLFraming) {
				t.Fatalf("error = %v, want framing error", err)
			}
			if output.String() != "334 \r\n" {
				t.Fatalf("unexpected reply after framing error: %q", output.String())
			}
			if ss.authOK {
				t.Fatal("framing error must not authenticate the session")
			}
		})
	}
}

func TestAuthRejectionsKeepSessionUsable(t *testing.T) {
	backend, err := userauth.NewStatic("alice@example.com:s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		submission bool
		backend    userauth.Backend
		greeting   string
		command    string
		response   string
	}{
		{
			name: "smtp listener", backend: backend, greeting: "EHLO client.example",
			command: "AUTH PLAIN", response: "502 AUTH is not supported",
		},
		{
			name: "before greeting", submission: true, backend: backend,
			command: "AUTH PLAIN", response: "503 send EHLO first",
		},
		{
			name: "HELO is insufficient", submission: true, backend: backend, greeting: "HELO client.example",
			command: "AUTH PLAIN", response: "503 send EHLO first",
		},
		{
			name: "missing backend", submission: true, greeting: "EHLO client.example",
			command: "AUTH PLAIN", response: "454 authentication backend unavailable",
		},
		{
			name: "bearer requires TLS", submission: true, backend: backend, greeting: "EHLO client.example",
			command: "AUTH XOAUTH2", response: "538 encryption required for XOAUTH2",
		},
		{
			name: "missing mechanism", submission: true, backend: backend, greeting: "EHLO client.example",
			command: "AUTH", response: "501 AUTH requires mechanism",
		},
		{
			name: "unsupported mechanism", submission: true, backend: backend, greeting: "EHLO client.example",
			command: "AUTH SCRAM-SHA-256", response: "501 unsupported auth mechanism",
		},
		{
			name: "malformed credentials", submission: true, backend: backend, greeting: "EHLO client.example",
			command: "AUTH PLAIN invalid", response: "501 invalid base64 credentials",
		},
		{
			name: "invalid credentials", submission: true, backend: backend, greeting: "EHLO client.example",
			command:  "AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHdyb25n",
			response: "535 authentication credentials invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{
				cfg:         config.Config{Hostname: "sub.example.test", SubmissionAuth: true, SubmissionAllowInsecureAuth: true},
				submission:  tt.submission,
				authBackend: tt.backend,
			}
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			go s.handleConn(server)
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			if tt.greeting != "" {
				mustWriteSMTPLine(t, w, tt.greeting)
				readSMTPResponse(t, r)
			}
			mustWriteSMTPLine(t, w, tt.command)
			if response, _ := readSMTPResponse(t, r); response != tt.response {
				t.Fatalf("response = %q, want %q", response, tt.response)
			}
			mustWriteSMTPLine(t, w, "NOOP")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatalf("NOOP code = %d, want 250", code)
			}
			mustWriteSMTPLine(t, w, "QUIT")
			if _, code := readSMTPResponse(t, r); code != 221 {
				t.Fatalf("QUIT code = %d, want 221", code)
			}
		})
	}
}
