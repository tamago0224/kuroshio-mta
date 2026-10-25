package deliveryhistory

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsSearchesAndPrunes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.db")
	now := time.Now().UTC()
	store, err := Open(path, time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{MessageID: "expired", Result: "accepted", OccurredAt: now.Add(-2 * time.Hour)},
		{MessageID: "m1", Recipient: "a@example.test", Result: "failed", Category: "transport", OccurredAt: now},
		{MessageID: "m1", Recipient: "a@example.test", Attempt: 1, Result: "delivered", TLSApplied: true, OccurredAt: now.Add(time.Second)},
	} {
		if err := store.Record(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path, time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.Search(t.Context(), Query{MessageID: "m1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Result != "failed" || page.NextAfter == 0 {
		t.Fatalf("first page = %+v", page)
	}
	next, err := store.Search(t.Context(), Query{MessageID: "m1", Limit: 1, AfterID: page.NextAfter})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].Result != "delivered" || !next.Items[0].TLSApplied || next.NextAfter != 0 {
		t.Fatalf("second page = %+v", next)
	}
	expired, err := store.Search(t.Context(), Query{MessageID: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired.Items) != 0 {
		t.Fatalf("expired history retained: %+v", expired.Items)
	}
}

func TestStoreRejectsOversizedPage(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "history.db"), time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Search(t.Context(), Query{Limit: maxPageSize + 1}); err == nil {
		t.Fatal("oversized page accepted")
	}
}

func TestDiagnosticBoundsHistory(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "history.db"), time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	diagnostic, err := store.CreateDiagnostic(t.Context(), "m1", time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{"retry", "delivered"} {
		if err := store.Record(t.Context(), Event{MessageID: "m1", Result: result}); err != nil {
			t.Fatal(err)
		}
	}
	got, page, err := store.DiagnosticHistory(t.Context(), diagnostic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Events != 1 || len(page.Items) != 1 {
		t.Fatalf("diagnostic=%+v page=%+v", got, page)
	}
}

func TestOpenMigratesTLSAppliedColumn(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE delivery_history (
id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT NOT NULL, recipient TEXT NOT NULL DEFAULT '',
attempt INTEGER NOT NULL, result TEXT NOT NULL, category TEXT NOT NULL DEFAULT '', stage TEXT NOT NULL DEFAULT '',
smtp_code INTEGER NOT NULL DEFAULT 0, route TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT '',
tls_required INTEGER NOT NULL DEFAULT 0, detail TEXT NOT NULL DEFAULT '', occurred_at TEXT NOT NULL,
UNIQUE(message_id, recipient, attempt, result, occurred_at));`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Record(t.Context(), Event{MessageID: "m1", Result: "delivered", TLSApplied: true}); err != nil {
		t.Fatal(err)
	}
	page, err := store.Search(t.Context(), Query{MessageID: "m1"})
	if err != nil || len(page.Items) != 1 || !page.Items[0].TLSApplied {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
