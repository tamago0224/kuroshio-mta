package queue

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/model"
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
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.retry", "m2.json")); err != nil {
		t.Fatalf("retry topic file not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.inbound", "m2.json")); !os.IsNotExist(err) {
		t.Fatalf("inbound file should be moved after retry, err=%v", err)
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
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.dlq", "m2.json")); err != nil {
		t.Fatalf("dlq topic file not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.retry", "m2.json")); !os.IsNotExist(err) {
		t.Fatalf("retry file should be removed after fail, err=%v", err)
	}
	due, err = s.Due(10)
	if err != nil {
		t.Fatalf("due2: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("failed message should not be pending")
	}
}

func TestStoreDueReadsLegacyPending(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg := &model.Message{
		ID:          "legacy1",
		MailFrom:    "sender@example.com",
		RcptTo:      []string{"r@example.net"},
		Data:        []byte("x"),
		CreatedAt:   time.Now().UTC().Add(-time.Minute),
		UpdatedAt:   time.Now().UTC().Add(-time.Minute),
		NextAttempt: time.Now().UTC().Add(-time.Second),
	}
	if err := os.MkdirAll(filepath.Join(d, "queue", "pending"), 0o755); err != nil {
		t.Fatalf("mkdir legacy pending: %v", err)
	}
	if err := s.write(filepath.Join(d, "queue", "pending", "legacy1.json"), msg); err != nil {
		t.Fatalf("write legacy message: %v", err)
	}

	due, err := s.Due(10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].ID != "legacy1" {
		t.Fatalf("expected legacy message due, got=%v", due)
	}
}
