package smtp

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

func TestSubmissionGreetingAndRSETPreserveAuthentication(t *testing.T) {
	for _, command := range []string{"RSET", "EHLO next.example", "HELO next.example"} {
		t.Run(command, func(t *testing.T) {
			backend, err := userauth.NewStatic("alice@example.com:s3cr3t")
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{
				cfg: config.Config{
					Hostname: "sub.example.test", SubmissionAuth: true, SubmissionAllowInsecureAuth: true,
					SubmissionSenderID: true,
				},
				submission: true, authBackend: backend,
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
			steps := []struct {
				command string
				code    int
			}{
				{command: "EHLO client.example", code: 250},
				{command: "AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHMzY3IzdA==", code: 235},
				{command: "MAIL FROM:<alice@example.com> BODY=8BITMIME", code: 250},
				{command: "RCPT TO:<bob@example.net>", code: 250},
				{command: command, code: 250},
				{command: "DATA", code: 503},
				{command: "RCPT TO:<bob@example.net>", code: 503},
				// Authenticated principal and sender restrictions survive the reset.
				{command: "MAIL FROM:<other@example.net>", code: 553},
				{command: "MAIL FROM:<alice@example.com>", code: 250},
				{command: "QUIT", code: 221},
			}
			for _, step := range steps {
				mustWriteSMTPLine(t, w, step.command)
				if _, code := readSMTPResponse(t, r); code != step.code {
					t.Fatalf("%s: code = %d, want %d", step.command, code, step.code)
				}
			}
		})
	}
}
