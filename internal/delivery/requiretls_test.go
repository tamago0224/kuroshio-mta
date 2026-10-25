package delivery

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDeliverTransactionRequiresTLSAndExtension(t *testing.T) {
	msg := &model.Message{RequireTLS: true}
	tests := []struct {
		name    string
		session *smtpSession
	}{
		{name: "no TLS", session: &smtpSession{requireTLS: true}},
		{name: "no extension", session: &smtpSession{tls: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(config.Config{}).deliverTransaction(context.Background(), tt.session, msg, "user@example.test")
			var smtpErr *SMTPResponseError
			if !errors.As(err, &smtpErr) || smtpErr.Code != 550 || smtpErr.Stage != StageTLS {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestDeliverTransactionSendsRequireTLSParameter(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	command := make(chan string, 1)
	go func() {
		r := bufio.NewReader(server)
		line, _ := r.ReadString('\n')
		command <- strings.TrimSpace(line)
		_, _ = server.Write([]byte("421 rejected\r\n"))
	}()
	session := &smtpSession{conn: client, r: bufio.NewReader(client), w: bufio.NewWriter(client), tls: true, requireTLS: true}
	msg := &model.Message{MailFrom: "sender@example.test", RequireTLS: true}
	_, _ = NewClient(config.Config{}).deliverTransaction(context.Background(), session, msg, "user@example.test")
	if got := <-command; got != "MAIL FROM:<sender@example.test> REQUIRETLS" {
		t.Fatalf("MAIL = %q", got)
	}
}

func TestRequireTLSRejectsUnprotectedFinalDeliveryModes(t *testing.T) {
	msg := &model.Message{RequireTLS: true}
	for _, mode := range []string{"local_spool", "lmtp"} {
		t.Run(mode, func(t *testing.T) {
			route := config.DeliveryRoute{Name: "local", Domains: []string{"example.test"}, Mode: mode}
			if mode == "lmtp" {
				route.LMTPUnixSocket = "/tmp/unused-lmtp.sock"
			}
			cfg := config.Config{DeliveryMode: mode, DeliveryRoutes: []config.DeliveryRoute{route}}
			client := NewClient(cfg)
			err := client.Deliver(context.Background(), msg, "user@example.test")
			var smtpErr *SMTPResponseError
			if !errors.As(err, &smtpErr) || smtpErr.Code != 550 {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}
