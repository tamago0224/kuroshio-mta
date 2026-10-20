package queue

import (
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"testing"
)

func TestLocalQueueRestoresTrustedSigningIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(&model.Message{ID: "signed", SigningIdentity: &model.SigningIdentity{Domain: "sender.test"}}); err != nil {
		t.Fatal(err)
	}
	s, err = New(root)
	if err != nil {
		t.Fatal(err)
	}
	due, err := s.Due(10)
	if err != nil || len(due) != 1 || due[0].SigningIdentity == nil || due[0].SigningIdentity.Domain != "sender.test" {
		t.Fatal("local restart lost trusted signing identity")
	}
}
