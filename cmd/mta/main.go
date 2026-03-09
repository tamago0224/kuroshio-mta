package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/observability"
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/smtp"
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

	errCh := make(chan error, 3)
	go func() { errCh <- s.Run(ctx) }()
	go func() { errCh <- d.Run(ctx) }()
	go func() { errCh <- observability.RunServer(ctx, cfg.ObservabilityAddr, metrics) }()

	for i := 0; i < 3; i++ {
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			continue
		}
		log.Printf("runtime error: %v", err)
		stop()
	}
}
