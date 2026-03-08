package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	"github.com/tamago/orinoco-mta/internal/config"
	"github.com/tamago/orinoco-mta/internal/delivery"
	"github.com/tamago/orinoco-mta/internal/queue"
	"github.com/tamago/orinoco-mta/internal/smtp"
	"github.com/tamago/orinoco-mta/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	q, err := queue.New(cfg.QueueDir)
	if err != nil {
		log.Fatalf("queue init failed: %v", err)
	}

	d := worker.New(cfg, q, delivery.NewClient(cfg))
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
