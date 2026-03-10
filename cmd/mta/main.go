package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/observability"
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/smtp"
	"github.com/tamago0224/orinoco-mta/internal/userauth"
	"github.com/tamago0224/orinoco-mta/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	q, err := queue.NewBackend(cfg)
	if err != nil {
		log.Fatalf("queue init failed: %v", err)
	}
	defer func() {
		if cErr := q.Close(); cErr != nil {
			log.Printf("queue close error: %v", cErr)
		}
	}()
	sup, err := bounce.NewSuppressionStore(filepath.Join(cfg.QueueDir, "suppression.json"))
	if err != nil {
		log.Fatalf("suppression init failed: %v", err)
	}
	metrics := observability.NewMetrics()

	d := worker.New(cfg, q, delivery.NewClient(cfg), sup, metrics)
	s := smtp.NewServer(cfg, q, metrics)
	var submissionServer *smtp.Server
	if cfg.SubmissionAddr != "" {
		authBackend, aErr := userauth.NewStatic(cfg.SubmissionUsers)
		if aErr != nil {
			log.Fatalf("submission auth init failed: %v", aErr)
		}
		if cfg.SubmissionAuth && strings.TrimSpace(cfg.SubmissionUsers) == "" {
			log.Fatalf("submission auth is required but MTA_SUBMISSION_USERS is empty")
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
		log.Printf("runtime error: %v", err)
		stop()
	}
}
