package eventwebhook

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

const SchemaVersion = "kuroshio.delivery-event.v1"

var ErrCapacity = errors.New("event webhook outbox capacity exceeded")

type Event struct {
	SchemaVersion string    `json:"schema_version"`
	EventID       string    `json:"event_id"`
	MessageID     string    `json:"message_id"`
	RecipientID   string    `json:"recipient_id,omitempty"`
	Result        string    `json:"result"`
	OccurredAt    time.Time `json:"occurred_at"`
	Attempt       int       `json:"attempt"`
	Category      string    `json:"category,omitempty"`
}

type record struct {
	Event
	Attempts    int       `json:"delivery_attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Outbox struct {
	mu           sync.Mutex
	pending, dlq string
	maxPending   int
	pendingCount int
	metrics      *observability.Metrics
}

func Open(root string, maxPending int, metrics *observability.Metrics) (*Outbox, error) {
	o := &Outbox{pending: filepath.Join(root, "pending"), dlq: filepath.Join(root, "dlq"), maxPending: maxPending, metrics: metrics}
	for _, dir := range []string{o.pending, o.dlq} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(o.pending)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			o.pendingCount++
		}
	}
	o.updatePendingGaugeLocked()
	return o, nil
}

func NewEvent(msg *model.Message, rcpt, result, category string, now time.Time) (Event, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Event{}, err
	}
	return Event{SchemaVersion: SchemaVersion, EventID: hex.EncodeToString(idBytes), MessageID: msg.ID, RecipientID: recipientID(rcpt), Result: result, OccurredAt: now.UTC(), Attempt: msg.Attempts, Category: category}, nil
}

func recipientID(rcpt string) string {
	if strings.TrimSpace(rcpt) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(rcpt))))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (o *Outbox) RecordAccepted(msg *model.Message) error {
	for _, rcpt := range msg.RcptTo {
		event, err := NewEvent(msg, rcpt, "accepted", "", time.Now())
		if err != nil {
			return err
		}
		if err := o.Enqueue(event); err != nil {
			return err
		}
	}
	return nil
}

func (o *Outbox) Enqueue(event Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := validateEvent(event); err != nil {
		return err
	}
	if o.pendingCount >= o.maxPending {
		return ErrCapacity
	}
	now := time.Now().UTC()
	r := record{Event: event, NextAttempt: now, CreatedAt: now}
	if err := o.write(filepath.Join(o.pending, event.EventID+".json"), &r); err != nil {
		return err
	}
	o.pendingCount++
	o.metric("event_webhook_enqueued")
	if o.metrics != nil {
		o.metrics.SetGauge("event_webhook_pending", int64(o.pendingCount))
	}
	return nil
}

func (o *Outbox) due(now time.Time, limit int) ([]record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entries, err := os.ReadDir(o.pending)
	if err != nil {
		return nil, err
	}
	out := make([]record, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var r record
		body, err := os.ReadFile(filepath.Join(o.pending, entry.Name()))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &r); err != nil || validateEvent(r.Event) != nil || r.EventID+".json" != entry.Name() {
			target := filepath.Join(o.dlq, entry.Name()+".corrupt")
			if moveErr := os.Rename(filepath.Join(o.pending, entry.Name()), target); moveErr != nil {
				return nil, moveErr
			}
			o.pendingCount--
			o.metric("event_webhook_corrupt")
			continue
		}
		if !r.NextAttempt.After(now) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextAttempt.Before(out[j].NextAttempt) })
	if len(out) > limit {
		out = out[:limit]
	}
	o.updatePendingGaugeLocked()
	return out, nil
}

func validateEvent(event Event) error {
	if event.SchemaVersion != SchemaVersion || event.MessageID == "" || len(event.EventID) != 32 {
		return errors.New("invalid event identity")
	}
	if _, err := hex.DecodeString(event.EventID); err != nil {
		return errors.New("invalid event identity")
	}
	switch event.Result {
	case "accepted", "delivered", "temporary_failure", "permanent_failure", "delivery_unknown":
	default:
		return errors.New("invalid event result")
	}
	return nil
}

func (o *Outbox) ack(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	err := os.Remove(filepath.Join(o.pending, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		o.pendingCount--
		o.updatePendingGaugeLocked()
	}
	return err
}

func (o *Outbox) retry(r record, delay time.Duration, reason string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	r.Attempts++
	r.NextAttempt = time.Now().UTC().Add(delay)
	r.LastError = reason
	return o.write(filepath.Join(o.pending, r.EventID+".json"), &r)
}

func (o *Outbox) fail(r record, reason string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	r.Attempts++
	r.LastError = reason
	if err := o.write(filepath.Join(o.dlq, r.EventID+".json"), &r); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(o.pending, r.EventID+".json")); err != nil {
		return err
	}
	o.pendingCount--
	o.updatePendingGaugeLocked()
	return nil
}

func (o *Outbox) sweep(now time.Time, retention time.Duration) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, dir := range []string{o.pending, o.dlq} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if now.Sub(info.ModTime()) > retention {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
				if dir == o.pending {
					o.pendingCount--
				}
			}
		}
	}
	o.updatePendingGaugeLocked()
	return nil
}

func (o *Outbox) write(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".event-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("persist event: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (o *Outbox) metric(name string) {
	if o.metrics != nil {
		o.metrics.Counter(name).Inc()
	}
}

func (o *Outbox) updatePendingGaugeLocked() {
	if o.metrics == nil {
		return
	}
	o.metrics.SetGauge("event_webhook_pending", int64(o.pendingCount))
}
