package reputation

import (
	"testing"
	"time"
)

func TestTrackerBlocksByWarmupLimit(t *testing.T) {
	tr := New(Config{
		StartDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		WarmupRules: []WarmupStep{
			{AfterDays: 0, Limit: 2},
		},
		MinSamples: 1,
	})
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if ok, _ := tr.Admit("gmail.com", now); !ok {
		t.Fatal("first admit should pass")
	}
	if ok, _ := tr.Admit("gmail.com", now); !ok {
		t.Fatal("second admit should pass")
	}
	if ok, reason := tr.Admit("gmail.com", now); ok || reason != "warmup_limit" {
		t.Fatalf("third admit ok=%v reason=%q", ok, reason)
	}
}

func TestTrackerBlocksByBounceRate(t *testing.T) {
	tr := New(Config{
		BounceThreshold:    0.5,
		ComplaintThreshold: 0.9,
		MinSamples:         2,
	})
	now := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC)
	if ok, _ := tr.Admit("gmail.com", now); !ok {
		t.Fatal("admit1 should pass")
	}
	tr.ObserveDelivery("gmail.com", false, true)
	if ok, _ := tr.Admit("gmail.com", now); !ok {
		t.Fatal("admit2 should pass before threshold check on next attempt")
	}
	tr.ObserveDelivery("gmail.com", false, true)
	if ok, reason := tr.Admit("gmail.com", now); ok || reason != "bounce_rate" {
		t.Fatalf("admit3 ok=%v reason=%q", ok, reason)
	}
}

func TestTrackerRecordsComplaintAndTLSReport(t *testing.T) {
	tr := New(Config{MinSamples: 1})
	now := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC)
	if ok, _ := tr.Admit("example.net", now); !ok {
		t.Fatal("admit should pass")
	}
	tr.ObserveDelivery("example.net", false, false)
	tr.RecordComplaint("example.net")
	tr.RecordTLSReport("example.net", true)
	tr.RecordTLSReport("example.net", false)

	snap := tr.Snapshot(now)
	if len(snap) != 1 {
		t.Fatalf("snapshot len=%d want=1", len(snap))
	}
	if snap[0].Complaints != 1 || snap[0].TLSRPTSuccesses != 1 || snap[0].TLSRPTFailures != 1 {
		t.Fatalf("snapshot=%+v", snap[0])
	}
}
