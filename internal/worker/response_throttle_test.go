package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
)

func TestResponseThrottleTriggersAndPreservesManualPause(t *testing.T) {
	store, err := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	rule := config.ResponseThrottleRule{ID: "rate-limit", SMTPCode: 421, Stage: "greeting", Threshold: 2, Window: time.Minute, Pause: time.Hour}
	r := newResponseThrottle(config.ResponseThrottleConfig{Enabled: true, Rules: []config.ResponseThrottleRule{rule}}, store)
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	failure := &delivery.SMTPResponseError{Stage: delivery.StageGreeting, Code: 421, Line: "421 4.7.0 slow down"}
	if _, triggered, err := r.observe(context.Background(), "example.test", failure); err != nil || triggered {
		t.Fatalf("first observation = %v %v", triggered, err)
	}
	if _, triggered, err := r.observe(context.Background(), "example.test", failure); err != nil || !triggered {
		t.Fatalf("second observation = %v %v", triggered, err)
	}
	entry, err := store.Lookup(context.Background(), "example.test")
	if err != nil || entry.Actor != "auto:response-throttle:rate-limit" {
		t.Fatalf("automatic pause = %+v %v", entry, err)
	}

	manual := deliverypause.Entry{Domain: "manual.test", Actor: "operator", Reason: "maintenance", ExpiresAt: now.Add(2 * time.Hour)}
	if err := store.Put(context.Background(), manual, false); err != nil {
		t.Fatal(err)
	}
	_, _, _ = r.observe(context.Background(), "manual.test", failure)
	_, _, _ = r.observe(context.Background(), "manual.test", failure)
	entry, _ = store.Lookup(context.Background(), "manual.test")
	if entry.Actor != "operator" || entry.Reason != "maintenance" {
		t.Fatalf("manual pause overwritten: %+v", entry)
	}
}

func TestResponseThrottleDryRunAndWindow(t *testing.T) {
	store, _ := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	rule := config.ResponseThrottleRule{ID: "mail-rate", Domains: []string{"target.test"}, SMTPCode: 451, Stage: "mail", Threshold: 2, Window: time.Minute, Pause: time.Hour}
	r := newResponseThrottle(config.ResponseThrottleConfig{Enabled: true, DryRun: true, Rules: []config.ResponseThrottleRule{rule}}, store)
	now := time.Now().UTC()
	r.now = func() time.Time { return now }
	failure := &delivery.SMTPResponseError{Stage: delivery.StageMAIL, Code: 451, Line: "451 4.7.0 slow down"}
	_, _, _ = r.observe(context.Background(), "target.test", failure)
	now = now.Add(time.Minute)
	if _, triggered, _ := r.observe(context.Background(), "target.test", failure); triggered {
		t.Fatal("event at window boundary must expire")
	}
	if _, triggered, _ := r.observe(context.Background(), "target.test", failure); !triggered {
		t.Fatal("dry-run rule did not trigger")
	}
	entry, _ := store.Lookup(context.Background(), "target.test")
	if entry.Domain != "" {
		t.Fatalf("dry-run created pause: %+v", entry)
	}
}

func TestResponseThrottleDryRunNeedsNoPauseStore(t *testing.T) {
	rule := config.ResponseThrottleRule{ID: "observe", SMTPCode: 421, Threshold: 1, Window: time.Minute, Pause: time.Minute}
	r := newResponseThrottle(config.ResponseThrottleConfig{Enabled: true, DryRun: true, Rules: []config.ResponseThrottleRule{rule}}, nil)
	if r == nil {
		t.Fatal("dry-run response throttle was disabled without a pause store")
	}
	_, triggered, err := r.observe(context.Background(), "example.test", &delivery.SMTPResponseError{Code: 421})
	if err != nil || !triggered {
		t.Fatalf("dry-run observation = %v %v", triggered, err)
	}
}
