package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResponseThrottleConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "response_throttle:\n  enabled: true\n  dry_run: true\n  rules:\n    - id: rate-limit\n      domains: ['*']\n      smtp_code: 421\n      stage: greeting\n      threshold: 3\n      window: 1m\n      pause: 10m\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ResponseThrottle.Enabled || !cfg.ResponseThrottle.DryRun || len(cfg.ResponseThrottle.Rules) != 1 || cfg.ResponseThrottle.Rules[0].Pause != 10*time.Minute {
		t.Fatalf("config = %+v", cfg.ResponseThrottle)
	}
}

func TestResponseThrottleDefaultsToDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "response_throttle:\n  enabled: true\n  rules:\n    - id: rate-limit\n      smtp_code: 421\n      threshold: 1\n      window: 1m\n      pause: 1m\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ResponseThrottle.DryRun {
		t.Fatal("response throttle must default to dry-run")
	}
}

func TestResponseThrottleEnforcementRequiresPauseStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "response_throttle:\n  enabled: true\n  dry_run: false\n  rules:\n    - id: rate-limit\n      smtp_code: 421\n      threshold: 1\n      window: 1m\n      pause: 1m\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithPath(path); err == nil {
		t.Fatal("enforcement without delivery pause store was accepted")
	}
}
