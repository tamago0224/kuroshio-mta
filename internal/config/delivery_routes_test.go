package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryRoutesConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "delivery_routes:\n  - name: internal\n    domains: [corp.example]\n    mode: relay\n    relay_host: relay.example\n    relay_port: 587\n    relay_require_tls: true\n    relay_username: sender\n    relay_password_file: /run/secrets/relay\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DeliveryRoutes) != 1 || cfg.DeliveryRoutes[0].Name != "internal" {
		t.Fatalf("routes=%+v", cfg.DeliveryRoutes)
	}
}

func TestDeliveryRoutesRejectUnsafeOrAmbiguousConfiguration(t *testing.T) {
	tests := []Config{
		{DeliveryRoutes: []DeliveryRoute{{Name: "auth", Domains: []string{"example.test"}, Mode: "relay", RelayHost: "relay.test", RelayUsername: "u", RelayPasswordFile: "secret"}}},
		{DeliveryRoutes: []DeliveryRoute{{Name: "duplicate", Domains: []string{"a.test"}, Mode: "mx"}, {Name: "duplicate", Domains: []string{"b.test"}, Mode: "mx"}}},
		{DeliveryRoutes: []DeliveryRoute{{Name: "bad", Domains: []string{"a.test"}, Mode: "unknown"}}},
	}
	for i, cfg := range tests {
		if err := cfg.validateDeliveryRoutes(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
