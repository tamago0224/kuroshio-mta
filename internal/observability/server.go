package observability

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/reputation"
)

func RunServer(ctx context.Context, addr string, m *Metrics) error {
	return RunServerWithReputation(ctx, addr, m, nil)
}

func RunServerWithReputation(ctx context.Context, addr string, m *Metrics, rep *reputation.Tracker) error {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if m == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(m.RenderPrometheus()))
	})
	mux.HandleFunc("/slo", func(w http.ResponseWriter, r *http.Request) {
		targets := LoadSLOTargetsFromEnv()
		snapshot := map[string]uint64{}
		if m != nil {
			snapshot = m.Snapshot()
		}
		report := EvaluateSLO(snapshot, targets)
		w.Header().Set("Content-Type", "application/json")
		if report.Status == "breach" {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(report.JSON())
	})
	mux.HandleFunc("/reputation", func(w http.ResponseWriter, r *http.Request) {
		if rep == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rep.JSON(time.Now().UTC()))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("observability listening", "component", "observability", "listen_addr", addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
