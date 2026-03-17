package observability

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/reputation"
)

func TestRunServerMetricsEndpoint(t *testing.T) {
	m := NewMetrics()
	m.Counter("smtp_connections").Inc()
	m.Counter("worker_delivery_success").Add(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := "127.0.0.1:29090"
	go func() {
		_ = RunServer(ctx, addr, m)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", resp.StatusCode)
			}
			if len(b) == 0 {
				t.Fatal("empty metrics body")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics endpoint not ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Setenv("MTA_SLO_MIN_DELIVERY_SUCCESS_RATE", "0.5")
	t.Setenv("MTA_SLO_MAX_RETRY_RATE", "0.5")
	t.Setenv("MTA_SLO_MAX_QUEUE_BACKLOG", "10000")
	resp, err := http.Get("http://" + addr + "/slo")
	if err != nil {
		t.Fatalf("slo endpoint not ready: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("slo status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	b, _ := io.ReadAll(resp.Body)
	if len(b) == 0 {
		t.Fatal("empty slo body")
	}
}

func TestRunServerReputationEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rep := reputation.New(reputation.Config{MinSamples: 1})
	if ok, _ := rep.Admit("gmail.com", time.Now().UTC()); !ok {
		t.Fatal("admit should pass")
	}
	rep.ObserveDelivery("gmail.com", false, false)

	addr := "127.0.0.1:29091"
	go func() {
		_ = RunServerWithReputation(ctx, addr, NewMetrics(), rep)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/reputation")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d", resp.StatusCode)
			}
			if len(b) == 0 {
				t.Fatal("empty reputation body")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reputation endpoint not ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
