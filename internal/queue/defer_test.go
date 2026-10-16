package queue

import (
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDeferPreservesAttemptsAndReloads(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	msg := &model.Message{ID: "defer", Attempts: 12, LastError: "prior delivery failure", RcptTo: []string{"user@example.test"}}
	if err := s.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	if err := s.Defer(msg, time.Hour); err != nil {
		t.Fatal(err)
	}
	restored, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	due, err := restored.Due(10)
	if err != nil || len(due) != 0 {
		t.Fatalf("deferred message still due: %v %v", due, err)
	}
	stored, err := restored.ListState("retry", 0)
	if err != nil || len(stored) != 1 {
		t.Fatalf("lost defer: %v", err)
	}
	if stored[0].Attempts != 12 || stored[0].LastError != msg.LastError {
		t.Fatalf("defer consumed attempt or replaced diagnostic: %+v", stored[0])
	}
}
