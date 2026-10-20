package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuarantineConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, backend string
		cfg           QuarantineConfig
		valid         bool
	}{
		{name: "legacy", backend: "kafka", valid: true},
		{name: "enabled local", backend: "local", cfg: QuarantineConfig{Enabled: true, Retention: time.Hour}, valid: true},
		{name: "enabled Kafka", backend: "kafka", cfg: QuarantineConfig{Enabled: true, Retention: time.Hour}},
		{name: "missing retention", backend: "local", cfg: QuarantineConfig{Enabled: true}},
		{name: "negative", cfg: QuarantineConfig{Retention: -time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(tc.backend); (err == nil) != tc.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("quarantine:\n  enabled: true\n  retention: 24h\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Quarantine.Enabled || cfg.Quarantine.Retention != 24*time.Hour {
		t.Fatalf("config: %+v", cfg.Quarantine)
	}
}
