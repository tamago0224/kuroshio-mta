package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestQuarantineSurvivesRestartAndRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	msg := &model.Message{ID: "held", Data: []byte("X-Kuroshio-Quarantine: false\r\n\r\nbody"), Quarantine: &model.Quarantine{Reason: "auth_policy", ExpiresAt: time.Now().Add(time.Hour)}}
	if err := s.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	s, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	due, err := s.Due(10)
	if err != nil || len(due) != 0 {
		t.Fatalf("held message became due: %v %v", due, err)
	}
	items, err := s.ListState("quarantine", 10)
	if err != nil || len(items) != 1 || items[0].Quarantine.Reason != "auth_policy" {
		t.Fatalf("lost hold: %v %v", items, err)
	}
	if err := s.Retry(msg, time.Second, "retry"); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("retry bypassed hold: %v", err)
	}
	if _, err := s.RequeueFromState("inbound", msg.ID, time.Now()); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("inbound requeue bypassed hold: %v", err)
	}
	if _, err := s.RequeueFromState("quarantine", msg.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequeueFromState("quarantine", msg.ID, time.Now()); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("repeated release: %v", err)
	}
	due, err = s.Due(10)
	if err != nil || len(due) != 1 || due[0].Quarantine != nil {
		t.Fatalf("release: %v %v", due, err)
	}
}

func TestQuarantineCountsAgainstCapacity(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.limits = CapacityLimits{MaxMessages: 1}
	if err := s.Enqueue(&model.Message{ID: "held", Quarantine: &model.Quarantine{Reason: "auth_policy"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "second"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("hold escaped capacity: %v", err)
	}
	if _, err := s.RequeueFromState("quarantine", "held", time.Now()); err != nil {
		t.Fatalf("release should not require new capacity: %v", err)
	}
}

func TestQuarantineDoesNotTrustHeaders(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "legacy", Data: []byte("X-Kuroshio-Quarantine: true\r\n\r\nbody")}); err != nil {
		t.Fatal(err)
	}
	due, err := s.Due(10)
	if err != nil || len(due) != 1 {
		t.Fatalf("header changed queue policy: %v %v", due, err)
	}
}

func TestKafkaRejectsUnsupportedQuarantine(t *testing.T) {
	t.Parallel()
	k := &KafkaStore{}
	if err := k.Enqueue(&model.Message{ID: "held", Quarantine: &model.Quarantine{Reason: "auth_policy"}}); err == nil {
		t.Fatal("Kafka silently accepted unsupported hold")
	}
}

func TestQuarantineReleaseWriteFailurePreservesHold(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "held", Quarantine: &model.Quarantine{Reason: "auth_policy"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.inbound, "held.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequeueFromState("quarantine", "held", time.Now()); err == nil {
		t.Fatal("expected failed atomic write")
	}
	due, err := s.Due(10)
	if err != nil || len(due) != 0 {
		t.Fatalf("failed release made message due: %v %v", due, err)
	}
	held, err := s.ListState("quarantine", 10)
	if err != nil || len(held) != 1 {
		t.Fatalf("failed release lost hold: %v %v", held, err)
	}
}
