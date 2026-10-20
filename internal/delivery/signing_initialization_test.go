package delivery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestConfiguredSigningInitializationFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		domain, selector, key string
	}{
		{name: "partial domain", domain: "sender.test"},
		{name: "partial selector", selector: "s1"},
		{name: "missing key", domain: "sender.test", selector: "s1", key: "missing.pem"},
		{name: "invalid key", domain: "sender.test", selector: "s1", key: "invalid.pem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			keyPath := ""
			if tc.key != "" {
				keyPath = filepath.Join(root, tc.key)
			}
			if tc.key == "invalid.pem" {
				if err := os.WriteFile(keyPath, []byte("PRIVATE-KEY-SENTINEL"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.Config{DeliveryMode: "local_spool", LocalSpoolDir: filepath.Join(root, "spool"), DKIMSignDomain: tc.domain, DKIMSignSelector: tc.selector, DKIMPrivateKeyFile: keyPath}
			cl := NewClient(cfg)
			defer cl.Close()
			if err := cl.InitError(); err == nil {
				t.Fatal("configured invalid signing accepted at startup")
			} else if strings.Contains(err.Error(), "PRIVATE-KEY-SENTINEL") || strings.Contains(err.Error(), root) {
				t.Fatal("initialization error exposed key data or path")
			}
			msg := &model.Message{ID: "message", Data: []byte("From: a@sender.test\r\nSubject: test\r\n\r\nbody\r\n")}
			if err := cl.Deliver(context.Background(), msg, "user@recipient.test"); err == nil {
				t.Fatal("initialization failure delivered unsigned message")
			}
			if _, err := cl.prepareOutboundData(msg.Data); err == nil {
				t.Fatal("initialization failure prepared unsigned data")
			}
		})
	}
}
