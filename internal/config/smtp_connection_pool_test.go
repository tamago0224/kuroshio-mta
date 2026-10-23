package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSMTPConnectionPoolConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("smtp_connection_pool:\n  enabled: true\n  max_connections: 8\n  max_connections_per_key: 2\n  max_idle: 4\n  max_idle_per_key: 1\n  idle_timeout: 15s\n  max_lifetime: 2m\n  max_deliveries: 20\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SMTPConnectionPool
	if !got.Enabled || got.MaxConnections != 8 || got.MaxConnectionsKey != 2 || got.MaxIdle != 4 || got.MaxIdleKey != 1 || got.IdleTimeout != 15*time.Second || got.MaxLifetime != 2*time.Minute || got.MaxDeliveries != 20 {
		t.Fatalf("smtp connection pool = %+v", got)
	}
}

func TestSMTPConnectionPoolRejectsInvalidLimits(t *testing.T) {
	cfg := defaultSMTPConnectionPoolConfig()
	cfg.MaxConnectionsKey = cfg.MaxConnections + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
