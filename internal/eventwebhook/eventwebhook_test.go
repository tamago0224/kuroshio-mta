package eventwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDispatcherRetriesWithStableEventIDAndSignature(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, append(secret, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var firstID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		id := r.Header.Get("X-Kuroshio-Event-ID")
		if firstID == "" {
			firstID = id
		} else if id != firstID {
			t.Errorf("event id changed: %q != %q", id, firstID)
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(body)
		if got, want := r.Header.Get("X-Kuroshio-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
			t.Errorf("signature = %q", got)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	outbox, err := Open(t.TempDir(), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	event, _ := NewEvent(&model.Message{ID: "m1"}, "User@Example.test", "delivered", "", time.Now())
	if err := outbox.Enqueue(event); err != nil {
		t.Fatal(err)
	}
	cfg := config.EventWebhookConfig{Enabled: true, URL: server.URL, SecretFile: secretPath, AllowHTTPLoopback: true, Timeout: time.Second, ScanInterval: time.Millisecond, MaxPending: 10, MaxAttempts: 3, MaxRetryDelay: time.Millisecond, Retention: time.Hour}
	dispatcher, err := NewDispatcher(cfg, outbox, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.Close()
	dispatcher.dispatch(context.Background())
	time.Sleep(2 * time.Millisecond)
	dispatcher.dispatch(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if due, _ := outbox.due(time.Now().Add(time.Hour), 10); len(due) != 0 {
		t.Fatalf("pending = %d", len(due))
	}
}

func TestDispatcherStatusPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		wantPending, wantDLQ int
	}{
		{"success", 204, 0, 0}, {"rate limited", 429, 1, 0}, {"server error", 503, 1, 0}, {"authentication", 401, 0, 1}, {"redirect", 302, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "/other")
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			dir := t.TempDir()
			secretPath := filepath.Join(dir, "secret")
			os.WriteFile(secretPath, []byte("01234567890123456789012345678901"), 0o600)
			outbox, _ := Open(filepath.Join(dir, "events"), 10, nil)
			event, _ := NewEvent(&model.Message{ID: "m"}, "u@test", "delivered", "", time.Now())
			outbox.Enqueue(event)
			cfg := config.EventWebhookConfig{Enabled: true, URL: server.URL, SecretFile: secretPath, AllowHTTPLoopback: true, Timeout: time.Second, ScanInterval: time.Second, MaxPending: 10, MaxAttempts: 3, MaxRetryDelay: time.Second, Retention: time.Hour}
			d, err := NewDispatcher(cfg, outbox, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			d.dispatch(context.Background())
			pending, _ := os.ReadDir(outbox.pending)
			dlq, _ := os.ReadDir(outbox.dlq)
			if len(pending) != tc.wantPending || len(dlq) != tc.wantDLQ {
				t.Fatalf("pending=%d dlq=%d", len(pending), len(dlq))
			}
		})
	}
}

func TestPersistedDeliveryEventSurvivesRestartWithoutRecipientPII(t *testing.T) {
	root := t.TempDir()
	outbox, _ := Open(root, 2, nil)
	event, err := NewEvent(&model.Message{ID: "m1", Attempts: 2}, "Person@Example.test", "delivered", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if event.RecipientID == "" || event.RecipientID == "person@example.test" {
		t.Fatalf("recipient id = %q", event.RecipientID)
	}
	if err := outbox.Enqueue(event); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(root, 2, nil)
	records, err := reopened.due(time.Now().Add(time.Second), 10)
	if err != nil || len(records) != 1 || records[0].EventID != event.EventID {
		t.Fatalf("records=%v err=%v", records, err)
	}
	body, _ := json.Marshal(records[0].Event)
	if bytes.Contains(body, []byte("Person@Example.test")) {
		t.Fatal("recipient PII present")
	}
}

func TestRetryAfterSupportsSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	if got := retryAfter("12", now); got != 12*time.Second {
		t.Fatalf("seconds = %s", got)
	}
	if got := retryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Fatalf("date = %s", got)
	}
}

func TestOutboxCapacityIsBoundedAcrossRestart(t *testing.T) {
	root := t.TempDir()
	outbox, err := Open(root, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := NewEvent(&model.Message{ID: "m1"}, "a@test", "accepted", "", time.Now())
	second, _ := NewEvent(&model.Message{ID: "m2"}, "b@test", "accepted", "", time.Now())
	if err := outbox.Enqueue(first); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Enqueue(second); !errors.Is(err, ErrCapacity) {
		t.Fatalf("Enqueue = %v", err)
	}
}

func TestOutboxRetentionRemovesExpiredPendingAndDLQ(t *testing.T) {
	outbox, err := Open(t.TempDir(), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	event, _ := NewEvent(&model.Message{ID: "m"}, "u@test", "delivered", "", time.Now())
	if err := outbox.Enqueue(event); err != nil {
		t.Fatal(err)
	}
	records, _ := outbox.due(time.Now().Add(time.Second), 1)
	if err := outbox.fail(records[0], "terminal"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, dir := range []string{outbox.pending, outbox.dlq} {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Add an expired pending event as well.
	pending, _ := NewEvent(&model.Message{ID: "p"}, "p@test", "accepted", "", time.Now())
	if err := outbox.Enqueue(pending); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outbox.pending, pending.EventID+".json")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := outbox.sweep(time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{outbox.pending, outbox.dlq} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatalf("%s entries=%d", dir, len(entries))
		}
	}
}

func TestDispatcherRetriesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("01234567890123456789012345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	outbox, _ := Open(filepath.Join(dir, "events"), 10, nil)
	event, _ := NewEvent(&model.Message{ID: "m"}, "u@test", "delivered", "", time.Now())
	outbox.Enqueue(event)
	cfg := config.EventWebhookConfig{Enabled: true, URL: server.URL, SecretFile: secretPath, AllowHTTPLoopback: true, Timeout: 5 * time.Millisecond, ScanInterval: time.Second, MaxPending: 10, MaxAttempts: 3, MaxRetryDelay: time.Second, Retention: time.Hour}
	d, err := NewDispatcher(cfg, outbox, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.dispatch(context.Background())
	pending, _ := os.ReadDir(outbox.pending)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
}

func TestCorruptRecordMovesAsideWithoutBlockingValidEvents(t *testing.T) {
	outbox, err := Open(t.TempDir(), 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outbox.pending, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	outbox.pendingCount++
	event, _ := NewEvent(&model.Message{ID: "m"}, "u@test", "accepted", "", time.Now())
	if err := outbox.Enqueue(event); err != nil {
		t.Fatal(err)
	}
	records, err := outbox.due(time.Now().Add(time.Second), 10)
	if err != nil || len(records) != 1 || records[0].EventID != event.EventID {
		t.Fatalf("records=%v err=%v", records, err)
	}
	dlq, _ := os.ReadDir(outbox.dlq)
	if len(dlq) != 1 {
		t.Fatalf("dlq=%d", len(dlq))
	}
}
