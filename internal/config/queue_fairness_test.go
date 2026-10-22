package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQueueFairnessConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "queue_fairness:\n  enabled: true\n  prefetch_factor: 8\n  max_tenants: 100\n  tenant_max_per_batch: 2\n  class_weights: {transactional: 4, bulk: 1, default: 1}\n  class_max_per_batch: {bulk: 2}\n  tenant_classes: {newsletter@example.test: bulk}\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.QueueFairness.Enabled || cfg.QueueFairness.ClassWeights["transactional"] != 4 || cfg.QueueFairness.TenantClasses["newsletter@example.test"] != "bulk" {
		t.Fatalf("config = %+v", cfg.QueueFairness)
	}
}
