package bounce

import (
	"path/filepath"
	"testing"
)

func TestSuppressionStoreAddAndPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "suppression.json")
	s, err := NewSuppressionStore(p)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := s.Add("User@Example.com", "hard bounce"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !s.IsSuppressed("user@example.com") {
		t.Fatal("address should be suppressed")
	}

	s2, err := NewSuppressionStore(p)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if !s2.IsSuppressed("user@example.com") {
		t.Fatal("suppression should persist")
	}
}
