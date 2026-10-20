package deliverypause

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalRestartExpiryResumeAndDryRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pauses.json")
	s, err := NewLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	e := Entry{Domain: "EXAMPLE.TEST.", Reason: "DNS maintenance", Actor: "ops", ExpiresAt: now.Add(time.Hour)}
	if err := s.Put(ctx, e, true); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.List(ctx); len(all) != 0 {
		t.Fatal("dry run changed state")
	}
	if err := s.Put(ctx, e, false); err != nil {
		t.Fatal(err)
	}
	s, err = NewLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	got, err := s.Lookup(ctx, "example.test")
	if err != nil || got.Domain != "example.test" {
		t.Fatalf("restart lost hold: %+v %v", got, err)
	}
	if _, err := s.Remove(ctx, "example.test", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Lookup(ctx, "example.test"); got.Domain == "" {
		t.Fatal("dry run resumed hold")
	}
	now = now.Add(time.Hour)
	if got, _ := s.Lookup(ctx, "example.test"); got.Domain != "" {
		t.Fatal("hold survived expiry boundary")
	}
	e.ExpiresAt = now.Add(time.Hour)
	if err := s.Put(ctx, e, false); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Remove(ctx, "example.test", false); !ok || err != nil {
		t.Fatalf("resume: %v %v", ok, err)
	}
	s, err = NewLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Lookup(ctx, "example.test"); got.Domain != "" {
		t.Fatal("resume not persisted")
	}
}
func TestInvalidAndFailedWriteDoNotPause(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	s, err := NewLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"*.example.test", "../path", "a..test", "-bad.test", "bad\n.test", "é.test"} {
		t.Run(domain, func(t *testing.T) {
			if _, err := NormalizeDomain(domain); err == nil {
				t.Fatal("invalid domain accepted")
			}
		})
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, Entry{Domain: "a.test", Reason: "maintenance", Actor: "ops", ExpiresAt: time.Now().Add(time.Hour)}, false); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got, _ := s.Lookup(ctx, "a.test"); got.Domain != "" {
		t.Fatal("failed write changed active state")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.List(canceled); err == nil {
		t.Fatal("canceled operation accepted")
	}
}

func TestLocalCapacityAndInvalidState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour)
	for i := range MaxEntries {
		domain := fmt.Sprintf("domain%d.test", i)
		s.entries[domain] = Entry{Domain: domain, Actor: "ops", Reason: "maintenance", ExpiresAt: expiry}
	}
	e := Entry{Domain: "extra.test", Actor: "ops", Reason: "maintenance", ExpiresAt: expiry}
	if err := s.Put(ctx, e, true); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity not enforced in dry run: %v", err)
	}
	for _, reason := range []string{"", "injected\nline"} {
		e.Reason = reason
		if err := s.Put(ctx, e, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid reason accepted: %v", err)
		}
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"wrong.test":{"domain":"other.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(path); err == nil {
		t.Fatal("corrupt state silently ignored")
	}
}
