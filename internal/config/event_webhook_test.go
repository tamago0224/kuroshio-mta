package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventWebhookConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "event_webhook:\n  enabled: true\n  url: https://events.example.test/v1/delivery\n  secret_file: /run/secrets/events\n  timeout: 3s\n  scan_interval: 2s\n  max_pending: 50\n  max_attempts: 4\n  max_retry_delay: 5m\n  retention: 48h\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.EventWebhook
	if !got.Enabled || got.Timeout != 3*time.Second || got.ScanInterval != 2*time.Second || got.MaxPending != 50 || got.MaxAttempts != 4 || got.MaxRetryDelay != 5*time.Minute || got.Retention != 48*time.Hour {
		t.Fatalf("event webhook = %+v", got)
	}
}

func TestEventWebhookRejectsUnsafeURL(t *testing.T) {
	cfg := defaultEventWebhookConfig()
	cfg.Enabled = true
	cfg.URL = "http://example.test/hook"
	cfg.SecretFile = "secret"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
