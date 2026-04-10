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
	observed, ok := b.(*observedBackend)
	if !ok {
		t.Fatalf("expected observed backend wrapper, got %T", b)
	}
	if _, ok := observed.next.(*Store); !ok {
		t.Fatalf("expected wrapped *Store backend, got %T", observed.next)
	}
}

func TestNewBackendUnknown(t *testing.T) {
	if _, err := NewBackend(config.Config{QueueBackend: "unknown"}); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}
