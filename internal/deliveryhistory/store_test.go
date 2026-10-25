package deliveryhistory

import (
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
		{MessageID: "m1", Recipient: "a@example.test", Attempt: 1, Result: "delivered", OccurredAt: now.Add(time.Second)},
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
	if len(next.Items) != 1 || next.Items[0].Result != "delivered" || next.NextAfter != 0 {
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
