package queue

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
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

func TestStoreEnqueueDeduplicatesByMessageID(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	m1 := &model.Message{ID: "dup1", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(m1); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	m2 := &model.Message{ID: "dup1", MailFrom: "other@example.com", RcptTo: []string{"other@example.net"}, Data: []byte("y")}
	if err := s.Enqueue(m2); err != nil {
		t.Fatalf("enqueue duplicate: %v", err)
	}
	due, err := s.Due(10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due count=%d want=1", len(due))
	}
	if due[0].MailFrom != "sender@example.com" {
		t.Fatalf("duplicate enqueue must keep original message, got=%q", due[0].MailFrom)
	}
}

func TestStoreRejectsRetryAfterAckState(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg := &model.Message{ID: "m3", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	due, err := s.Due(1)
	if err != nil || len(due) != 1 {
		t.Fatalf("due=%v err=%v", due, err)
	}
	if err := s.AckSent("m3", due[0]); err != nil {
		t.Fatalf("ack sent: %v", err)
	}
	if err := s.Retry(due[0], time.Minute, "temp"); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("retry err=%v want=%v", err, ErrInvalidStateTransition)
	}
}

func TestStoreQuarantinesPoisonMessage(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "queue")
	s, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	path := filepath.Join(root, "mail.inbound", "bad1.json")
	if err := os.WriteFile(path, []byte("{invalid-json"), 0o644); err != nil {
		t.Fatalf("write poison: %v", err)
	}
	due, err := s.Due(10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("poison should not become due")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("poison source must be removed, err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "mail.dlq", "poison"))
	if err != nil {
		t.Fatalf("read poison dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("poison file count=%d want=1", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".bad") {
		t.Fatalf("poison file suffix unexpected: %s", entries[0].Name())
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

func TestStoreRequeueFromRetryAndDLQ(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	retryMsg := &model.Message{ID: "m-retry", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(retryMsg); err != nil {
		t.Fatalf("enqueue retry: %v", err)
	}
	if err := s.Retry(retryMsg, time.Hour, "temp"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := s.RequeueFromState("retry", "m-retry", time.Now()); err != nil {
		t.Fatalf("requeue retry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.inbound", "m-retry.json")); err != nil {
		t.Fatalf("requeued inbound not found: %v", err)
	}

	dlqMsg := &model.Message{ID: "m-dlq", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(dlqMsg); err != nil {
		t.Fatalf("enqueue dlq: %v", err)
	}
	if err := s.Fail(dlqMsg, "perm"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if _, err := s.RequeueFromState("dlq", "m-dlq", time.Now()); err != nil {
		t.Fatalf("requeue dlq: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "queue", "mail.inbound", "m-dlq.json")); err != nil {
		t.Fatalf("requeued inbound from dlq not found: %v", err)
	}
}

func TestStoreListState(t *testing.T) {
	d := t.TempDir()
	s, err := New(filepath.Join(d, "queue"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	msg := &model.Message{ID: "m-list", MailFrom: "sender@example.com", RcptTo: []string{"r@example.net"}, Data: []byte("x")}
	if err := s.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s.Retry(msg, time.Hour, "temp"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	list, err := s.ListState("retry", 10)
	if err != nil {
		t.Fatalf("list retry: %v", err)
	}
	if len(list) != 1 || list[0].ID != "m-list" {
		t.Fatalf("list=%v", list)
	}
}
