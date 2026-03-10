package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/logging"
	"github.com/tamago0224/orinoco-mta/internal/observability"
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/smtp"
	"github.com/tamago0224/orinoco-mta/internal/userauth"
	"github.com/tamago0224/orinoco-mta/internal/worker"
)

func main() {
	cfg := config.Load()
	slog.SetDefault(logging.New(cfg.LogLevel, os.Stdout))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	q, err := queue.NewBackend(cfg)
	if err != nil {
		fatal("queue init failed", "error", err)
	}
	defer func() {
		if cErr := q.Close(); cErr != nil {
			slog.Error("queue close failed", "component", "main", "error", cErr)
		}
	}()
	sup, err := bounce.NewSuppressionStore(filepath.Join(cfg.QueueDir, "suppression.json"))
	if err != nil {
		fatal("suppression init failed", "error", err)
	}
	metrics := observability.NewMetrics()

	d := worker.New(cfg, q, delivery.NewClient(cfg), sup, metrics)
	s := smtp.NewServer(cfg, q, metrics)
	var submissionServer *smtp.Server
	if cfg.SubmissionAddr != "" {
		authBackend, aErr := userauth.NewStatic(cfg.SubmissionUsers)
		if aErr != nil {
			fatal("submission auth init failed", "error", aErr)
		}
		if cfg.SubmissionAuth && strings.TrimSpace(cfg.SubmissionUsers) == "" {
			fatal("submission auth is required but MTA_SUBMISSION_USERS is empty")
		}
		submissionServer = smtp.NewSubmissionServer(cfg, q, metrics, authBackend)
	}

	workers := 3
	if submissionServer != nil {
		workers++
	}
	errCh := make(chan error, workers)
	go func() { errCh <- s.Run(ctx) }()
	if submissionServer != nil {
		go func() { errCh <- submissionServer.Run(ctx) }()
	}
	go func() { errCh <- d.Run(ctx) }()
	go func() { errCh <- observability.RunServer(ctx, cfg.ObservabilityAddr, metrics) }()

	for i := 0; i < workers; i++ {
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			continue
		}
		slog.Error("runtime error", "component", "main", "error", err)
		stop()
	}
}

func fatal(msg string, args ...any) {
	base := []any{"component", "main"}
	base = append(base, args...)
	slog.Error(msg, base...)
	os.Exit(1)
}
