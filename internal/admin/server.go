package admin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
)

func RunServer(ctx context.Context, addr, tokenConfig string, suppressions *bounce.SuppressionStore, queue queueManager) error {
	if addr == "" {
		return nil
	}
	api := NewAPI(suppressions, queue, tokenConfig)
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
