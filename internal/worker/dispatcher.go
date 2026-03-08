package worker

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/tamago/orinoco-mta/internal/config"
	"github.com/tamago/orinoco-mta/internal/delivery"
	"github.com/tamago/orinoco-mta/internal/model"
	"github.com/tamago/orinoco-mta/internal/queue"
)

type Dispatcher struct {
	cfg   config.Config
	queue *queue.Store
	cl    *delivery.Client
}

func New(cfg config.Config, q *queue.Store, cl *delivery.Client) *Dispatcher {
	return &Dispatcher{cfg: cfg, queue: q, cl: cl}
}

func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.processBatch(ctx); err != nil {
				log.Printf("worker batch error: %v", err)
			}
		}
	}
}

func (d *Dispatcher) processBatch(ctx context.Context) error {
	msgs, err := d.queue.Due(d.cfg.WorkerCount * 4)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	sem := make(chan struct{}, d.cfg.WorkerCount)
	wg := sync.WaitGroup{}
	for _, m := range msgs {
		msg := m
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			d.handleMessage(ctx, msg)
		}()
	}
	wg.Wait()
	return nil
}

func (d *Dispatcher) handleMessage(ctx context.Context, msg *model.Message) {
	var errs []string
	for _, rcpt := range msg.RcptTo {
		if err := d.cl.Deliver(ctx, msg, rcpt); err != nil {
			errs = append(errs, rcpt+": "+err.Error())
		}
	}

	if len(errs) == 0 {
		if err := d.queue.AckSent(msg.ID, msg); err != nil {
			log.Printf("ack sent error id=%s: %v", msg.ID, err)
		}
		return
	}

	reason := strings.Join(errs, "; ")
	if isPermanent(reason) || msg.Attempts >= 12 {
		if err := d.queue.Fail(msg, reason); err != nil {
			log.Printf("mark failed error id=%s: %v", msg.ID, err)
		}
		return
	}
	delay := backoff(msg.Attempts)
	if err := d.queue.Retry(msg, delay, reason); err != nil {
		log.Printf("retry schedule error id=%s: %v", msg.ID, err)
	}
}

func backoff(attempts int) time.Duration {
	switch attempts {
	case 0:
		return 5 * time.Minute
	case 1:
		return 30 * time.Minute
	case 2:
		return 2 * time.Hour
	case 3:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func isPermanent(reason string) bool {
	return strings.Contains(reason, "code=5") || errors.Is(context.Canceled, context.Canceled)
}
