package queue

import (
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestPauseStatsCountsMessagesOnce(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []*model.Message{
		{ID: "mixed", RcptTo: []string{"user@a.test", "second@a.test", "user@b.test"}},
		{ID: "other", RcptTo: []string{"user@b.test"}},
		{ID: "isolated", RcptTo: []string{"user@a.test"}, Quarantine: &model.Quarantine{Reason: "auth_policy"}},
	} {
		if err := s.Enqueue(msg); err != nil {
			t.Fatal(err)
		}
	}
	count, oldest, err := s.PauseStats(map[string]bool{"a.test": true}, time.Now().Add(time.Hour))
	if err != nil || count != 1 || oldest < time.Hour {
		t.Fatalf("stats: %d %v %v", count, oldest, err)
	}
}
