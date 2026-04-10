package admin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
)

func RunServer(ctx context.Context, addr, tokenConfig string, suppressions *bounce.SuppressionStore, queue queueManager, rep *reputation.Tracker) error {
	return RunServerWithBackend(ctx, addr, NewConfigTokenBackend(tokenConfig), suppressions, queue, rep)
}

func RunServerWithBackend(ctx context.Context, addr string, authBackend AuthBackend, suppressions *bounce.SuppressionStore, queue queueManager, rep *reputation.Tracker) error {
	if addr == "" {
		return nil
	}
	api := NewAPIWithBackend(suppressions, queue, rep, authBackend)
	srv := &http.Server{Addr: addr, Handler: api.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("admin api listening", "component", "admin", "listen_addr", addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
