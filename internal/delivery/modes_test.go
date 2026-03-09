package delivery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/model"
)

func TestDeliverLocalSpoolWritesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m1", MailFrom: "sender@example.com", Data: []byte("Subject: hi\r\n\r\nhello")}

	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver local spool: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected spool file")
	}
	p := filepath.Join(dir, entries[0].Name())
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("spool file is empty")
	}
}

func TestDeliverRelayUsesConfiguredTarget(t *testing.T) {
	cfg := config.Config{DeliveryMode: "relay", RelayHost: "relay.example.net", RelayPort: 2525, RelayRequireTLS: true}
	cl := NewClient(cfg)
	called := false
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, requireTLS bool) error {
		called = true
		if host != "relay.example.net" || port != 2525 {
			t.Fatalf("unexpected relay target host=%s port=%d", host, port)
		}
		if !requireTLS {
			t.Fatal("relay require tls must be true")
		}
		return nil
	}
	msg := &model.Message{ID: "m2", MailFrom: "sender@example.com", Data: []byte("x")}
	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver relay: %v", err)
	}
	if !called {
		t.Fatal("relay deliverHostFn should be called")
	}
}
