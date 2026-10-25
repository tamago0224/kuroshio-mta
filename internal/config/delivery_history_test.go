package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliveryHistoryConfiguration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("delivery_history:\n  enabled: true\n  retention: 48h\n  max_rows: 42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DeliveryHistory.Enabled || cfg.DeliveryHistory.Retention != 48*time.Hour || cfg.DeliveryHistory.MaxRows != 42 {
		t.Fatalf("delivery history = %+v", cfg.DeliveryHistory)
	}
}

func TestDeliveryHistoryRejectsInvalidLimits(t *testing.T) {
	t.Parallel()
	cfg := defaultDeliveryHistoryConfig()
	cfg.MaxRows = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("zero max_rows accepted")
	}
}
