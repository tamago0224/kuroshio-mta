package queue

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago/orinoco-mta/internal/model"
)

func TestStoreLifecycle(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg := &model.Message{ID: "m1", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	due, err := s.Due(10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].ID != "m1" {
		t.Fatalf("due=%v", due)
	}
	if err := s.AckSent("m1", due[0]); err != nil {
		t.Fatalf("ack sent: %v", err)
	}
	due, err = s.Due(10)
	if err != nil {
		t.Fatalf("due2: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected empty due queue, got=%d", len(due))
	}
}

func TestStoreRetryAndFail(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg := &model.Message{ID: "m2", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s.Retry(msg, time.Hour, "temporary"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	due, err := s.Due(10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("message should not be due yet")
	}
	if err := s.Fail(msg, "permanent"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	due, err = s.Due(10)
	if err != nil {
		t.Fatalf("due2: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("failed message should not be pending")
	}
}
