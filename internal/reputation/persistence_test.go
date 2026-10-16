package reputation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentTrackerRestoresCountersAndDeduplication(t *testing.T) {
	now := time.Now().UTC()
	cfg := Config{StartDate: now.Add(-24 * time.Hour), MinSamples: 1, ComplaintThreshold: 0.1}
	path := filepath.Join(t.TempDir(), "reputation.json")
	tracker, err := NewPersistent(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := tracker.Admit("example.net", now); !ok {
		t.Fatal(why)
	}
	tracker.ObserveDelivery("example.net", false, false)
	report, err := ParseComplaint([]byte(testComplaint), "message/feedback-report", "provider/123")
	if err != nil {
		t.Fatal(err)
	}
	if accepted, err := tracker.Ingest(report, now); err != nil || !accepted {
		t.Fatalf("ingest: %v %v", accepted, err)
	}
	restored, err := NewPersistent(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := restored.Snapshot(now)
	if len(snapshot) != 1 || snapshot[0].Successes != 1 || snapshot[0].TodayAttempts != 1 || snapshot[0].Complaints != 2 {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	if ok, why := restored.Admit("example.net", now); ok || why != "complaint_rate" {
		t.Fatalf("lost block decision: %v %s", ok, why)
	}
	if accepted, err := restored.Ingest(report, now); err != nil || accepted {
		t.Fatalf("duplicate: %v %v", accepted, err)
	}
	if restored.Snapshot(now)[0].Complaints != 2 {
		t.Fatal("duplicate counted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "user@") || strings.Contains(string(data), "provider/123") {
		t.Fatal("PII or raw report id persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}

func TestPersistentTrackerRollsBackFailedReport(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	path := filepath.Join(blocker, "state.json")
	tracker, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Report{ID: reportIdentity("test", "1"), Counts: map[string]ReportCounts{"example.com": {Complaints: 1}}}
	if accepted, err := tracker.Ingest(report, time.Now()); err == nil || accepted {
		t.Fatal("storage failure was acknowledged")
	}
	if len(tracker.Snapshot(time.Now())) != 0 {
		t.Fatal("failed report modified counters")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if accepted, err := tracker.Ingest(report, time.Now()); err != nil || !accepted {
		t.Fatalf("retry: %v %v", accepted, err)
	}
	if tracker.Snapshot(time.Now())[0].Complaints != 1 {
		t.Fatal("retry counted twice")
	}
}

func TestPersistentTrackerFlushAndCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tracker, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	tracker.RecordTLSReport("example.com", true)
	tracker.RecordTLSReport("example.com", false)
	if err := tracker.Flush(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	got := restored.Snapshot(time.Now())[0]
	if got.TLSRPTSuccesses != 1 || got.TLSRPTFailures != 1 {
		t.Fatalf("TLS report counters: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := restored.RunPersistence(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := restored.RunPersistence(ctx, 0); err == nil {
		t.Fatal("invalid interval accepted")
	}
	if err := os.WriteFile(path, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistent(Config{}, path); err == nil {
		t.Fatal("corrupt state silently reset")
	}
}

func TestPersistentManualRecordingRollsBackStorageFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tracker, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordComplaint("example.com"); err != nil {
		t.Fatal(err)
	}
	tracker.statePath = filepath.Join(path, "cannot-write.json")
	if err := tracker.RecordComplaint("example.com"); err == nil {
		t.Fatal("failed manual complaint acknowledged")
	}
	if err := tracker.RecordTLSReport("example.com", true); err == nil {
		t.Fatal("failed manual TLS report acknowledged")
	}
	got := tracker.Snapshot(time.Now())[0]
	if got.Complaints != 1 || got.TLSRPTSuccesses != 0 {
		t.Fatalf("failed updates were not rolled back: %+v", got)
	}
	tracker.statePath = path
	if err := tracker.RecordComplaint("example.com"); err != nil {
		t.Fatal(err)
	}
	restored, err := NewPersistent(Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot(time.Now())[0].Complaints != 2 {
		t.Fatal("retry was counted twice")
	}
}

func TestConcurrentReportDeduplication(t *testing.T) {
	tracker, err := NewPersistent(Config{}, filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := Report{ID: reportIdentity("test", "same"), Counts: map[string]ReportCounts{"example.com": {Complaints: 1}}}
	type result struct {
		accepted bool
		err      error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			accepted, err := tracker.Ingest(report, time.Now())
			results <- result{accepted: accepted, err: err}
		}()
	}
	count := 0
	for range 8 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.accepted {
			count++
		}
	}
	if count != 1 || tracker.Snapshot(time.Now())[0].Complaints != 1 {
		t.Fatalf("accepted %d copies", count)
	}
}

func TestPersistentWarmupLimitSurvivesRestartAndResetsDaily(t *testing.T) {
	now := time.Now().UTC()
	cfg := Config{StartDate: now.Add(-time.Hour), WarmupRules: []WarmupStep{{AfterDays: 0, Limit: 1}}}
	path := filepath.Join(t.TempDir(), "state.json")
	tracker, err := NewPersistent(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, reason := tracker.Admit("example.com", now); !ok {
		t.Fatal(reason)
	}
	if err := tracker.Flush(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewPersistent(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, reason := restored.Admit("example.com", now); ok || reason != "warmup_limit" {
		t.Fatalf("lost warmup limit: %v %s", ok, reason)
	}
	if ok, reason := restored.Admit("example.com", now.Add(24*time.Hour)); !ok {
		t.Fatalf("new day did not reset limit: %s", reason)
	}
}
