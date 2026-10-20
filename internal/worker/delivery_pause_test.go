package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func TestDeliveryPausePreservesAttemptsAndPartialDelivery(t *testing.T) {
	host, port, count := acceptingSMTPServer(t)
	cfg := config.Config{DeliveryMode: "relay", RelayHost: host, RelayPort: port, DialTimeout: time.Second, SendTimeout: time.Second, MaxRetryAge: time.Hour}
	q, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	holds, err := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := holds.Put(context.Background(), deliverypause.Entry{Domain: "a.test", Actor: "ops", Reason: "maintenance", ExpiresAt: time.Now().Add(time.Hour)}, false); err != nil {
		t.Fatal(err)
	}
	cl := delivery.NewClient(cfg)
	defer cl.Close()
	d := &Dispatcher{cfg: cfg, queue: q, cl: cl, pauses: holds}
	msg := &model.Message{ID: "mixed", RcptTo: []string{"user@a.test", "user@b.test"}, Data: []byte("Subject: test\r\n\r\nhello")}
	if err := q.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(context.Background(), msg)
	if count.Load() != 1 {
		t.Fatal("other domain did not progress")
	}
	retry, err := q.ListState("retry", 0)
	if err != nil || len(retry) != 1 {
		t.Fatalf("pending %v %v", retry, err)
	}
	if retry[0].Attempts != 0 || len(retry[0].RcptTo) != 1 || retry[0].RcptTo[0] != "user@a.test" {
		t.Fatalf("hold consumed attempt or retained delivered recipient: %+v", retry[0])
	}
	if _, err := holds.Remove(context.Background(), "a.test", false); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(context.Background(), retry[0])
	if count.Load() != 2 {
		t.Fatal("resume failed or successful recipient was redelivered")
	}
}

type failingPauseStore struct{ deliverypause.Store }

func (failingPauseStore) Lookup(context.Context, string) (deliverypause.Entry, error) {
	return deliverypause.Entry{}, errors.New("unavailable")
}
func TestDeliveryPauseBackendFailureDefersWithoutDial(t *testing.T) {
	q, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	msg := &model.Message{ID: "held", RcptTo: []string{"user@a.test"}}
	if err := q.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{queue: q, pauses: failingPauseStore{}}
	d.handleMessage(context.Background(), msg)
	pending, err := q.ListState("retry", 0)
	if err != nil || len(pending) != 1 || pending[0].Attempts != 0 {
		t.Fatalf("outage did not hold: %v %v", pending, err)
	}
}

func TestDeliveryPauseRespectsMessageLifetime(t *testing.T) {
	t.Parallel()
	q, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	holds, err := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := holds.Put(context.Background(), deliverypause.Entry{Domain: "a.test", Actor: "ops", Reason: "maintenance", ExpiresAt: time.Now().Add(time.Hour)}, false); err != nil {
		t.Fatal(err)
	}
	msg := &model.Message{ID: "expired", MailFrom: "sender@b.test", RcptTo: []string{"user@a.test"}, Data: []byte("From: sender@b.test\r\nSubject: test\r\n\r\nbody")}
	if err := q.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	msg.CreatedAt = time.Now().Add(-2 * time.Hour)
	d := &Dispatcher{cfg: config.Config{MaxRetryAge: time.Hour, Hostname: "mx.test"}, queue: q, pauses: holds}
	d.handleMessage(context.Background(), msg)
	sent, err := q.ListState("sent", 0)
	if err != nil || len(sent) != 1 || sent[0].Attempts != 0 {
		t.Fatalf("lifetime processing: %v %v", sent, err)
	}
	notifications, err := q.ListState("inbound", 0)
	if err != nil || len(notifications) != 1 || notifications[0].MailFrom != "" {
		t.Fatalf("expiry DSN: %v %v", notifications, err)
	}
}

func TestUnrelatedTerminalFailureDoesNotDiscardPause(t *testing.T) {
	t.Parallel()
	q, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	holds, err := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := holds.Put(context.Background(), deliverypause.Entry{Domain: "a.test", Actor: "ops", Reason: "maintenance", ExpiresAt: time.Now().Add(time.Hour)}, false); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DeliveryMode: "relay", MaxAttempts: 1}
	client := delivery.NewClient(cfg)
	defer client.Close()
	d := &Dispatcher{cfg: cfg, queue: q, cl: client, pauses: holds}
	// The missing relay host deterministically fails b.test without opening a socket.
	msg := &model.Message{ID: "mixed", Attempts: 1, RcptTo: []string{"user@a.test", "user@b.test"}}
	if err := q.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	d.handleMessage(context.Background(), msg)
	pending, err := q.ListState("retry", 0)
	if err != nil || len(pending) != 1 || len(pending[0].RcptTo) != 1 || pending[0].RcptTo[0] != "user@a.test" || pending[0].Attempts != 1 {
		t.Fatalf("terminal failure discarded pause: %v %v", pending, err)
	}
}
