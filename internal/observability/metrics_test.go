package observability

import (
	"strings"
	"testing"
)

func TestMetricsCounterAndRender(t *testing.T) {
	m := NewMetrics()
	m.Counter("smtp_connections").Inc()
	m.Counter("smtp_connections").Add(2)
	m.Counter("worker.delivery.success").Inc()

	s := m.Snapshot()
	if s["smtp_connections_total"] != 3 {
		t.Fatalf("smtp_connections_total=%d", s["smtp_connections_total"])
	}
	if s["worker_delivery_success_total"] != 1 {
		t.Fatalf("worker_delivery_success_total=%d", s["worker_delivery_success_total"])
	}

	r := m.RenderPrometheus()
	for _, k := range []string{"smtp_connections_total", "worker_delivery_success_total"} {
		if !strings.Contains(r, k) {
			t.Fatalf("missing %s in render: %s", k, r)
		}
	}
}

func TestGaugeCanDecreaseWithoutChangingCounters(t *testing.T) {
	m := NewMetrics()
	m.Counter("accepted").Inc()
	m.SetGauge("queue_pending_messages", 10)
	m.SetGauge("queue_pending_messages", 2)
	rendered := m.RenderPrometheus()
	if !strings.Contains(rendered, "# TYPE queue_pending_messages gauge\nqueue_pending_messages 2\n") {
		t.Fatal(rendered)
	}
	if m.Snapshot()["accepted_total"] != 1 {
		t.Fatal("counter snapshot changed")
	}
}
