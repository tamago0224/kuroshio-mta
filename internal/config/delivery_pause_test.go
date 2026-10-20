package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeliveryPauseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, backend string
		addrs         []string
		valid         bool
	}{
		{name: "local", backend: "local", valid: true},
		{name: "Redis", backend: "redis", addrs: []string{"127.0.0.1:6379"}, valid: true},
		{name: "Redis missing addresses", backend: "redis"},
		{name: "invalid backend", backend: "memory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := (DeliveryPauseConfig{Enabled: true, Backend: tc.backend}).Validate(tc.addrs); (err == nil) != tc.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("delivery_pause:\n  enabled: true\n  backend: local\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil || !cfg.DeliveryPause.Enabled || cfg.DeliveryPause.Backend != "local" {
		t.Fatalf("load: %+v %v", cfg.DeliveryPause, err)
	}
}
