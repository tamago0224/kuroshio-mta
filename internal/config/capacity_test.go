package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQueueCapacitySources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("queue_max_messages: 10\nqueue_max_bytes: 200\nqueue_low_watermark_percent: 60\nqueue_min_free_bytes: 300\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MTA_QUEUE_MAX_MESSAGES", "20")
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QueueMaxMessages != 20 || cfg.QueueMaxBytes != 200 || cfg.QueueLowWatermarkPercent != 60 || cfg.QueueMinFreeBytes != 300 {
		t.Fatal("unexpected capacity configuration")
	}
}

func TestQueueCapacityInvalidEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MTA_QUEUE_MAX_BYTES", "bad-limit")
	if _, err := LoadWithPath(path); err == nil {
		t.Fatal("malformed capacity silently disabled protection")
	}
}
