package worker

import (
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
)

func TestFairQueuePreventsBulkAndTenantStarvation(t *testing.T) {
	store, err := queue.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := range 20 {
		if err := store.Enqueue(&model.Message{ID: "bulk-" + string(rune('a'+i)), Tenant: "bulk-a", TrafficClass: "bulk"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, msg := range []*model.Message{{ID: "txn-a", Tenant: "tenant-a", TrafficClass: "transactional"}, {ID: "txn-b", Tenant: "tenant-b", TrafficClass: "transactional"}} {
		if err := store.Enqueue(msg); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.QueueFairnessConfig{Enabled: true, PrefetchFactor: 8, MaxTenants: 10, ClassWeights: map[string]int{"transactional": 4, "bulk": 1, "default": 1}}
	got, err := newFairQueue(cfg).due(store, 4)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, msg := range got {
		seen[msg.ID] = true
	}
	if !seen["txn-a"] || !seen["txn-b"] {
		t.Fatalf("transactional traffic starved: %v", seen)
	}
}

func TestFairQueueLegacyFallbackAndBatchLimits(t *testing.T) {
	store, _ := queue.New(t.TempDir())
	defer store.Close()
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Enqueue(&model.Message{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.QueueFairnessConfig{Enabled: true, PrefetchFactor: 2, MaxTenants: 2, TenantMaxPerBatch: 1, ClassWeights: map[string]int{"default": 1}}
	got, err := newFairQueue(cfg).due(store, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tenant != "default" || got[0].TrafficClass != "default" {
		t.Fatalf("legacy fallback/limit = %+v", got)
	}
}
