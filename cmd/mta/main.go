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
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/smtp"
	"github.com/tamago0224/orinoco-mta/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	q, err := queue.New(cfg.QueueDir)
	if err != nil {
		log.Fatalf("queue init failed: %v", err)
	}
	sup, err := bounce.NewSuppressionStore(filepath.Join(cfg.QueueDir, "suppression.json"))
	if err != nil {
		log.Fatalf("suppression init failed: %v", err)
	}

	d := worker.New(cfg, q, delivery.NewClient(cfg), sup)
	s := smtp.NewServer(cfg, q)

	errCh := make(chan error, 2)
	go func() { errCh <- s.Run(ctx) }()
	go func() { errCh <- d.Run(ctx) }()

	for i := 0; i < 2; i++ {
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			continue
		}
		log.Printf("runtime error: %v", err)
		stop()
	}
}
