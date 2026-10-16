package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMXShapingYAMLAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("mx_shaping:\n  enabled: true\n  backend: memory\n  cache_ttl: 30s\n  retry_delay: 2s\n  default:\n    messages_per_minute: 60\n    message_burst: 2\n    max_connections: 3\n  providers:\n    - name: shared\n      domains: [a.test, b.test]\n      limits:\n        max_connections: 1\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MXShaping.Enabled || cfg.MXShaping.CacheTTL != 30*time.Second || cfg.MXShaping.Default.MessageBurst != 2 || len(cfg.MXShaping.Providers) != 1 {
		t.Fatal("shaping YAML not loaded")
	}
	for _, tc := range []struct {
		name string
		cfg  MXShapingConfig
	}{
		{"unknown backend", MXShapingConfig{Enabled: true, Backend: "unknown"}},
		{"negative limit", MXShapingConfig{Enabled: true, Default: ShapingLimits{MaxConnections: -1}}},
		{"negative TTL", MXShapingConfig{Enabled: true, CacheTTL: -time.Second}},
		{"duplicate provider", MXShapingConfig{Enabled: true, Providers: []ShapingProvider{{Name: "same"}, {Name: "same"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("invalid shaping accepted")
			}
		})
	}
}
