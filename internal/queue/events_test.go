package queue

import (
	"errors"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

type failingAcceptedSink struct{}

func (failingAcceptedSink) RecordAccepted(*model.Message) error { return errors.New("full") }

func TestAcceptedEventFailureDoesNotRejectPersistedMail(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := WithAcceptedEvents(store, failingAcceptedSink{}, nil)
	msg := &model.Message{ID: "m1", RcptTo: []string{"user@example.test"}}
	if err := backend.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	due, err := backend.Due(1)
	if err != nil || len(due) != 1 || due[0].ID != "m1" {
		t.Fatalf("due=%v err=%v", due, err)
	}
}
