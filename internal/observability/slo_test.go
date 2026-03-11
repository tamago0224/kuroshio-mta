package observability

import "testing"

func TestEvaluateSLOOK(t *testing.T) {
	snap := map[string]uint64{
		"worker_delivery_success_total":  90,
		"worker_temporary_failure_total": 5,
		"worker_permanent_bounce_total":  5,
		"smtp_queued_messages_total":     100,
		"worker_ack_sent_total":          90,
		"worker_mark_failed_total":       10,
	}
	targets := SLOTargets{
		MinDeliverySuccessRate: 0.85,
		MaxRetryRate:           0.10,
		MaxQueueBacklog:        10,
	}
	r := EvaluateSLO(snap, targets)
	if r.Status != "ok" {
		t.Fatalf("status=%s breaches=%v", r.Status, r.Breaches)
	}
}

func TestEvaluateSLOBreach(t *testing.T) {
	snap := map[string]uint64{
		"worker_delivery_success_total":  10,
		"worker_temporary_failure_total": 20,
		"worker_permanent_bounce_total":  10,
		"smtp_queued_messages_total":     100,
		"worker_ack_sent_total":          5,
		"worker_mark_failed_total":       1,
	}
	targets := SLOTargets{
		MinDeliverySuccessRate: 0.80,
		MaxRetryRate:           0.20,
		MaxQueueBacklog:        50,
	}
	r := EvaluateSLO(snap, targets)
	if r.Status != "breach" {
		t.Fatalf("status=%s want=breach", r.Status)
	}
	if len(r.Breaches) == 0 {
		t.Fatal("expected breaches")
	}
}
