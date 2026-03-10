package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/observability"
	"github.com/tamago0224/orinoco-mta/internal/queue"
)

type Dispatcher struct {
	cfg   config.Config
	queue queue.Backend
	cl    *delivery.Client
	sup   *bounce.SuppressionStore
	m     *observability.Metrics
}

func New(cfg config.Config, q queue.Backend, cl *delivery.Client, sup *bounce.SuppressionStore, metrics *observability.Metrics) *Dispatcher {
	return &Dispatcher{cfg: cfg, queue: q, cl: cl, sup: sup, m: metrics}
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
				slog.Error("worker batch failed", "component", "worker", "error", err)
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
	var (
		errs    []error
		reasons []string
	)
	for _, rcpt := range msg.RcptTo {
		if d.sup != nil && d.sup.IsSuppressed(rcpt) {
			reasons = append(reasons, rcpt+": suppressed")
			d.metricInc("worker_suppressed_recipient")
			continue
		}
		if err := d.cl.Deliver(ctx, msg, rcpt); err != nil {
			reasons = append(reasons, rcpt+": "+err.Error())
			var smtpErr *delivery.SMTPResponseError
			if errors.As(err, &smtpErr) && smtpErr.Permanent() {
				d.metricInc("worker_permanent_bounce")
				if d.sup != nil {
					if sErr := d.sup.Add(rcpt, smtpErr.Line); sErr != nil {
						slog.Error("suppression add failed", "component", "worker", "rcpt", rcpt, "msg_id", msg.ID, "error", sErr)
					}
				}
				continue
			}
			d.metricInc("worker_temporary_failure")
			errs = append(errs, err)
		} else {
			d.metricInc("worker_delivery_success")
		}
	}

	if len(errs) == 0 {
		if err := d.queue.AckSent(msg.ID, msg); err != nil {
			slog.Error("ack sent failed", "component", "worker", "msg_id", msg.ID, "error", err)
		}
		d.metricInc("worker_ack_sent")
		return
	}

	reason := strings.Join(reasons, "; ")
	if shouldFail(msg, errs, d.cfg, time.Now().UTC()) {
		if err := d.queue.Fail(msg, reason); err != nil {
			slog.Error("mark failed failed", "component", "worker", "msg_id", msg.ID, "error", err)
		}
		d.metricInc("worker_mark_failed")
		return
	}
	delay := backoff(msg.Attempts, d.cfg.RetrySchedule)
	if err := d.queue.Retry(msg, delay, reason); err != nil {
		slog.Error("retry schedule failed", "component", "worker", "msg_id", msg.ID, "error", err)
	}
	d.metricInc("worker_retry_scheduled")
}

func backoff(attempts int, schedule []time.Duration) time.Duration {
	if len(schedule) == 0 {
		return 24 * time.Hour
	}
	if attempts < 0 {
		return schedule[0]
	}
	if attempts >= len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempts]
}

func shouldFail(msg *model.Message, errs []error, cfg config.Config, now time.Time) bool {
	if cfg.MaxAttempts > 0 && msg.Attempts >= cfg.MaxAttempts {
		return true
	}
	if cfg.MaxRetryAge > 0 && !msg.CreatedAt.IsZero() && now.Sub(msg.CreatedAt) >= cfg.MaxRetryAge {
		return true
	}
	if len(errs) == 0 {
		return false
	}
	hasTemporary := false
	hasPermanent := false
	for _, err := range errs {
		var smtpErr *delivery.SMTPResponseError
		if errors.As(err, &smtpErr) {
			if smtpErr.Temporary() {
				hasTemporary = true
			}
			if smtpErr.Permanent() {
				hasPermanent = true
			}
			continue
		}
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
			hasTemporary = true
			continue
		}
		// Unknown/network errors are treated as temporary.
		hasTemporary = true
	}
	if hasTemporary {
		return false
	}
	return hasPermanent
}

func (d *Dispatcher) metricInc(name string) {
	if d.m != nil {
		d.m.Counter(name).Inc()
	}
}
