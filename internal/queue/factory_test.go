package queue

import (
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestNewBackendLocal(t *testing.T) {
	b, err := NewBackend(config.Config{QueueBackend: "local", QueueDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewBackend(local): %v", err)
	}
	defer b.Close()
	if _, ok := b.(*Store); !ok {
		t.Fatalf("expected *Store backend, got %T", b)
	}
}

func TestNewBackendUnknown(t *testing.T) {
	if _, err := NewBackend(config.Config{QueueBackend: "unknown"}); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}
