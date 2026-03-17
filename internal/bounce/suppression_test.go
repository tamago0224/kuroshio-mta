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

func TestSuppressionStoreListAndRemove(t *testing.T) {
	p := filepath.Join(t.TempDir(), "suppression.json")
	s, err := NewSuppressionStore(p)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := s.Add("b@example.com", "bounce"); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := s.Add("a@example.com", "policy"); err != nil {
		t.Fatalf("add a: %v", err)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("list len=%d want=2", len(list))
	}
	if list[0].Address != "a@example.com" || list[1].Address != "b@example.com" {
		t.Fatalf("sorted list unexpected: %+v", list)
	}

	removed, err := s.Remove("A@example.com")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Fatal("expected remove=true")
	}
	if s.IsSuppressed("a@example.com") {
		t.Fatal("address should have been removed")
	}
}
