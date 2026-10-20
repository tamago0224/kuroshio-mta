package delivery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDeliverLMTPHandlesPerRecipientFinalResponses(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "lmtp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverResult := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 mailstore LMTP ready\r\n")
		if line, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(line, "LHLO ") {
			serverResult <- fmt.Errorf("LHLO = %q, %v", line, err)
			return
		}
		fmt.Fprint(conn, "250-mailstore\r\n250 8BITMIME\r\n")
		for _, prefix := range []string{"MAIL FROM:", "RCPT TO:", "RCPT TO:", "RCPT TO:"} {
			line, err := r.ReadString('\n')
			if err != nil || !strings.HasPrefix(line, prefix) {
				serverResult <- fmt.Errorf("expected %s, got %q, %v", prefix, line, err)
				return
			}
			fmt.Fprint(conn, "250 2.1.5 ok\r\n")
		}
		if line, err := r.ReadString('\n'); err != nil || line != "DATA\r\n" {
			serverResult <- fmt.Errorf("DATA = %q, %v", line, err)
			return
		}
		fmt.Fprint(conn, "354 send message\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				serverResult <- err
				return
			}
			if line == ".\r\n" {
				break
			}
		}
		fmt.Fprint(conn, "250 2.0.0 delivered\r\n451 4.2.0 mailbox busy\r\n550 5.1.1 unknown user\r\n")
		serverResult <- nil
	}()

	cfg := config.Config{Hostname: "mta.example.test", DialTimeout: time.Second, SendTimeout: 3 * time.Second, DeliveryRoutes: []config.DeliveryRoute{
		{Name: "mailstore", Domains: []string{"example.test"}, Mode: "lmtp", LMTPUnixSocket: socket},
	}}
	client := NewClient(cfg)
	defer client.Close()
	recipients := []string{"ok@example.test", "later@example.test", "bad@example.test"}
	results := client.DeliverBatch(context.Background(), &model.Message{MailFrom: "sender@example.test", Data: []byte("Subject: test\r\n\r\nbody")}, recipients)
	if results[recipients[0]] != nil {
		t.Fatalf("success = %v", results[recipients[0]])
	}
	if err := results[recipients[1]]; err == nil || !isTemporarySMTP(err) {
		t.Fatalf("temporary = %v", err)
	}
	if err := results[recipients[2]]; err == nil || !isPermanentSMTP(err) {
		t.Fatalf("permanent = %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestDeliverLMTPMissingFinalResponseIsTemporary(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "lmtp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 ready\r\n")
		r.ReadString('\n')
		fmt.Fprint(conn, "250 ok\r\n")
		for range 3 {
			r.ReadString('\n')
			fmt.Fprint(conn, "250 ok\r\n")
		}
		r.ReadString('\n')
		fmt.Fprint(conn, "354 send\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil || line == ".\r\n" {
				break
			}
		}
		fmt.Fprint(conn, "250 delivered\r\n")
	}()
	cfg := config.Config{Hostname: "mta.test", DialTimeout: time.Second, SendTimeout: time.Second, DeliveryRoutes: []config.DeliveryRoute{
		{Name: "mailstore", Domains: []string{"example.test"}, Mode: "lmtp", LMTPUnixSocket: socket},
	}}
	client := NewClient(cfg)
	defer client.Close()
	results := client.DeliverBatch(context.Background(), &model.Message{Data: []byte("x")}, []string{"one@example.test", "two@example.test"})
	if results["one@example.test"] != nil || results["two@example.test"] == nil {
		t.Fatalf("results = %v", results)
	}
	_ = listener.Close()
	_ = os.Remove(socket)
}

func isTemporarySMTP(err error) bool {
	var response *SMTPResponseError
	return errors.As(err, &response) && response.Temporary()
}

func isPermanentSMTP(err error) bool {
	var response *SMTPResponseError
	return errors.As(err, &response) && response.Permanent()
}
