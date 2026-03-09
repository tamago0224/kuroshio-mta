package observability

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestRunServerMetricsEndpoint(t *testing.T) {
	m := NewMetrics()
	m.Counter("smtp_connections").Inc()

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
}
