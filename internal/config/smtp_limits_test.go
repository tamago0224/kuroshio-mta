package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSMTPLimitsConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("smtp_limits:\n  max_connections: 10\n  max_connections_per_ip: 2\n  max_recipients: 150\n  max_messages: 4\n  max_bytes: 10000\n  max_auth_failures: 3\n  max_rcpt_failures: 6\n  idle_timeout: 30s\n  max_lifetime: 2m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SMTPLimits.WithDefaults()
	if got.MaxConnections != 10 || got.MaxConnectionsPerIP != 2 || got.MaxRecipients != 150 || got.MaxMessages != 4 || got.MaxBytes != 10000 || got.MaxAuthFailures != 3 || got.MaxRcptFailures != 6 || got.IdleTimeout != 30*time.Second || got.MaxLifetime != 2*time.Minute {
		t.Fatalf("limits = %+v", got)
	}
	if err := (SMTPLimitsConfig{MaxBytes: -1}).Validate(); err == nil {
		t.Fatal("negative bytes allowed")
	}
	if err := (SMTPLimitsConfig{IdleTimeout: -1}).Validate(); err == nil {
		t.Fatal("negative timeout allowed")
	}
}
