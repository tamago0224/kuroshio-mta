package dmarcreport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func testObservation(at time.Time) Observation {
	return Observation{
		ObservedAt: at, PolicyDomain: "example.test", AuthorDomain: "mail.example.test",
		Policy:    Policy{P: "reject", SP: "quarantine", ADKIM: "r", ASPF: "s"},
		Discovery: "treewalk", SourceIP: "192.0.2.7", Disposition: "reject",
		DKIMAligned: "fail", SPFAligned: "pass", EnvelopeFrom: "bounce.example.test",
		EnvelopeTo: "example.test", DKIMResults: []DKIMResult{{Domain: "signer.test", Selector: "s1", Result: "pass"}},
		SPFDomain: "bounce.example.test", SPFResult: "pass",
	}
}

type reportQueue struct {
	msgs map[string]*model.Message
	fail bool
}

func (q *reportQueue) Enqueue(msg *model.Message) error {
	if q.fail {
		return errors.New("queue unavailable")
	}
	if q.msgs == nil {
		q.msgs = make(map[string]*model.Message)
	}
	if _, exists := q.msgs[msg.ID]; !exists {
		q.msgs[msg.ID] = msg
	}
	return nil
}
func (*reportQueue) Due(int) ([]*model.Message, error)                 { return nil, nil }
func (*reportQueue) AckSent(string, *model.Message) error              { return nil }
func (*reportQueue) Retry(*model.Message, time.Duration, string) error { return nil }
func (*reportQueue) Defer(*model.Message, time.Duration) error         { return nil }
func (*reportQueue) Fail(*model.Message, string) error                 { return nil }
func (*reportQueue) Close() error                                      { return nil }

var _ queue.Backend = (*reportQueue)(nil)

func TestFlushQueuesStableDailyReportAndDeletesOnlyAfterSuccess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reports.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	ob := testObservation(day.Add(5 * time.Hour))
	ob.Recipients = []string{"mailto:agg@example.test"}
	if err := store.Add(ctx, ob); err != nil {
		t.Fatal(err)
	}
	failed := &reportQueue{fail: true}
	if err := store.Flush(ctx, failed, "mx.receiver.test", "postmaster@receiver.test", day.Add(24*time.Hour)); err == nil {
		t.Fatal("flush succeeded despite queue failure")
	}
	rows, err := store.Snapshot(ctx, day)
	if err != nil || len(rows) != 1 {
		t.Fatalf("failed queue removed period: rows=%v err=%v", rows, err)
	}
	q := &reportQueue{}
	if err := store.Flush(ctx, q, "mx.receiver.test", "postmaster@receiver.test", day.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(q.msgs) != 1 {
		t.Fatalf("queued messages=%d want 1", len(q.msgs))
	}
	for _, msg := range q.msgs {
		if msg.MailFrom != "" || len(msg.RcptTo) != 1 || msg.RcptTo[0] != "agg@example.test" {
			t.Fatalf("message envelope=%+v", msg)
		}
		for _, want := range []string{"Report Domain: example.test", "application/gzip", ".xml.gz"} {
			if !strings.Contains(string(msg.Data), want) {
				t.Errorf("message missing %q", want)
			}
		}
	}
	if err := store.Flush(ctx, q, "mx.receiver.test", "postmaster@receiver.test", day.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(q.msgs) != 1 {
		t.Fatalf("repeat flush created duplicate messages: %d", len(q.msgs))
	}
}

func TestEnqueueGroupSplitsOversizedXMLWithUniqueStableIDs(t *testing.T) {
	rows := make([]Aggregate, 1000)
	for i := range rows {
		signatures := make([]DKIMResult, 100)
		for j := range signatures {
			signatures[j] = DKIMResult{Domain: "signer.example.test", Selector: fmt.Sprintf("s-%d", j), Result: "pass"}
		}
		rows[i] = Aggregate{Count: 1, Observation: Observation{
			PolicyDomain: "example.test", AuthorDomain: "example.test", Policy: Policy{P: "reject"},
			Discovery: "treewalk", SourceIP: fmt.Sprintf("2001:db8::%x", i+1), Disposition: "none",
			DKIMAligned: "pass", SPFAligned: "fail", DKIMResults: signatures,
		}}
	}
	q := &reportQueue{}
	group := &flushGroup{policyDomain: "example.test", discovery: "treewalk", to: "agg@example.test", policy: Policy{P: "reject"}, rows: rows}
	period := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if err := (&Store{}).enqueueGroup(context.Background(), q, "mx.example.test", "postmaster@mx.example.test", period, group); err != nil {
		t.Fatal(err)
	}
	if len(q.msgs) < 2 {
		t.Fatalf("oversized XML was not split: reports=%d", len(q.msgs))
	}
	ids := make(map[string]struct{}, len(q.msgs))
	for id := range q.msgs {
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate report queue ID %q", id)
		}
		ids[id] = struct{}{}
	}
}

func TestStoreAggregatesByTupleAndPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reports.db")
	first := time.Date(2026, 10, 19, 1, 0, 0, 0, time.UTC)
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, testObservation(first)); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, testObservation(first.Add(22*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, testObservation(first.Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions=%o want 600", info.Mode().Perm())
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.Snapshot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Count != 2 {
		t.Fatalf("period rows=%+v", rows)
	}
	if rows[0].Observation.ObservedAt.Hour() != 0 {
		t.Fatalf("period timestamp=%s", rows[0].Observation.ObservedAt)
	}
	next, err := store.Snapshot(ctx, first.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].Count != 1 {
		t.Fatalf("next period rows=%+v", next)
	}
	if err := store.DeletePeriod(ctx, first); err != nil {
		t.Fatal(err)
	}
	rows, err = store.Snapshot(ctx, first)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted rows=%v err=%v", rows, err)
	}
}

func TestStoreConcurrentAdds(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const workers, perWorker = 6, 12
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				if err := store.Add(ctx, testObservation(time.Now())); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	rows, err := store.Snapshot(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Count != workers*perWorker {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestStoreRejectsInvalidObservationAndCanceledContext(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bad := testObservation(time.Now())
	bad.SourceIP = ""
	if err := store.Add(ctx, bad); err == nil {
		t.Fatal("accepted missing source IP")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Add(canceled, testObservation(time.Now())); err == nil {
		t.Fatal("accepted canceled context")
	}
}
