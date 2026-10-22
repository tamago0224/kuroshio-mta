package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
	"github.com/tamago0224/kuroshio-mta/internal/shaping"
	"github.com/tamago0224/kuroshio-mta/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Dispatcher struct {
	pauses    deliverypause.Store
	cfg       config.Config
	queue     queue.Backend
	cl        *delivery.Client
	sup       *bounce.SuppressionStore
	m         *observability.Metrics
	throttle  domainThrottle
	rep       *reputation.Tracker
	responses *responseThrottle
	fair      *fairQueue
}

var workerTracer = otel.Tracer("github.com/tamago0224/kuroshio-mta/internal/worker")

func New(cfg config.Config, q queue.Backend, cl *delivery.Client, sup *bounce.SuppressionStore, metrics *observability.Metrics, rep *reputation.Tracker) (*Dispatcher, error) {
	if err := cl.InitError(); err != nil {
		return nil, err
	}
	throttle, err := newDomainThrottle(cfg)
	if err != nil {
		return nil, err
	}
	return &Dispatcher{
		cfg:      cfg,
		queue:    q,
		cl:       cl,
		sup:      sup,
		m:        metrics,
		throttle: throttle,
		rep:      rep,
		fair:     newFairQueue(cfg.QueueFairness),
	}, nil
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
	d.refreshPauseStats(ctx)
	limit := d.cfg.WorkerCount * 4
	var msgs []*model.Message
	var err error
	if d.fair != nil {
		msgs, err = d.fair.due(d.queue, limit)
	} else {
		msgs, err = d.queue.Due(limit)
	}
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	if d.fair != nil && d.m != nil {
		classes, tenants, maxTenantDepth, oldest := d.fair.stats(time.Now())
		d.m.SetGauge("queue_fairness_buffered_messages", int64(d.fair.depth()))
		d.m.SetGauge("queue_fairness_active_tenants", tenants)
		d.m.SetGauge("queue_fairness_max_tenant_depth", maxTenantDepth)
		d.m.SetGauge("queue_fairness_oldest_age_seconds", oldest)
		for class := range d.cfg.QueueFairness.ClassWeights {
			d.m.SetGauge("queue_fairness_class_"+class+"_depth", 0)
		}
		for class, depth := range classes {
			d.m.SetGauge("queue_fairness_class_"+class+"_depth", depth)
		}
		for _, msg := range msgs {
			d.metricInc("queue_fairness_selected_class_" + msg.TrafficClass)
		}
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
	ctx = observability.ExtractTraceContext(ctx, msg)
	ctx, span := workerTracer.Start(ctx, "worker.handle_message")
	span.SetAttributes(
		attribute.String("mail.message_id", msg.ID),
		attribute.Int("mail.attempt", msg.Attempts),
		attribute.Int("mail.rcpt_count", len(msg.RcptTo)),
	)
	defer span.End()
	observability.InjectTraceContext(ctx, msg)

	pausedRecipients := map[string]bool{}
	failedRecipients := map[string]string{}
	var deferredUntil time.Time
	var remaining []string
	var (
		errs    []error
		reasons []string
	)
	type batchRecipient struct {
		rcpt    string
		domain  string
		release func()
	}
	lmtpBatches := map[string][]batchRecipient{}
	lmtpLeaseHeld := map[string]bool{}
	handleResult := func(rcpt, domain string, err error) {
		if err != nil {
			var deferred *shaping.DeferredError
			if errors.As(err, &deferred) {
				remaining = append(remaining, rcpt)
				until := time.Now().Add(deferred.Delay)
				if until.After(deferredUntil) {
					deferredUntil = until
				}
				d.metricInc("worker_shaping_deferred")
				return
			}
			reasons = append(reasons, rcpt+": "+err.Error())
			var smtpErr *delivery.SMTPResponseError
			smtpCode := 0
			if errors.As(err, &smtpErr) {
				smtpCode = smtpErr.Code
			}
			classification := delivery.ClassifyFailure(err)
			if d.responses != nil {
				ruleID, triggered, responseErr := d.responses.observe(ctx, domain, err)
				if ruleID != "" {
					d.metricInc("worker_response_throttle_match_" + ruleID)
				}
				if triggered {
					d.metricInc("worker_response_throttle_trigger_" + ruleID)
				}
				if responseErr != nil {
					d.metricInc("worker_response_throttle_error")
				}
			}
			d.metricInc("worker_bounce_class_" + string(classification.Category))
			d.metricInc("worker_bounce_rule_" + classification.Rule)
			slog.InfoContext(ctx, "delivery failure classified",
				"component", "audit", "event", "delivery_failure_classified",
				"msg_id", msg.ID, "rcpt", logging.MaskEmail(rcpt),
				"category", classification.Category, "rule", classification.Rule, "smtp_code", smtpCode,
				"stage", classification.Stage, "enhanced_status", classification.EnhancedStatus,
				"suppression_eligible", classification.Suppress)
			if errors.As(err, &smtpErr) && smtpErr.Permanent() {
				if d.rep != nil {
					d.rep.ObserveDelivery(domain, false, true)
				}
				d.emitHardBounceDSN(ctx, msg, rcpt, smtpErr.Error())
				if d.throttle != nil && d.throttle.observe(domain, false) {
					d.metricInc("worker_domain_throttle_backend_error")
					span.AddEvent("domain_throttle.backend_error", trace.WithAttributes(
						attribute.String("mail.domain", domain),
						attribute.String("domain_throttle.backend", d.cfg.DomainThrottleBackend),
						attribute.String("domain_throttle.stage", "observe"),
					))
				}
				d.metricInc("worker_permanent_bounce")
				if d.sup != nil && classification.Suppress {
					if suppressErr := d.sup.Add(rcpt, smtpErr.Line); suppressErr != nil {
						slog.ErrorContext(ctx, "suppression add failed", "component", "worker", "rcpt", logging.MaskEmail(rcpt), "msg_id", msg.ID, "error", suppressErr)
					} else {
						d.metricInc("worker_suppression_added")
					}
				}
				return
			}
			d.metricInc("worker_temporary_failure")
			if d.rep != nil {
				d.rep.ObserveDelivery(domain, true, false)
			}
			d.emitSoftBounceDSN(ctx, msg, rcpt, err.Error())
			if d.throttle != nil && d.throttle.observe(domain, true) {
				d.metricInc("worker_domain_throttle_backend_error")
				span.AddEvent("domain_throttle.backend_error", trace.WithAttributes(
					attribute.String("mail.domain", domain),
					attribute.String("domain_throttle.backend", d.cfg.DomainThrottleBackend),
					attribute.String("domain_throttle.stage", "observe"),
				))
			}
			errs = append(errs, err)
			failedRecipients[rcpt] = err.Error()
			remaining = append(remaining, rcpt)
			return
		}
		if d.rep != nil {
			d.rep.ObserveDelivery(domain, false, false)
		}
		if d.throttle != nil && d.throttle.observe(domain, false) {
			d.metricInc("worker_domain_throttle_backend_error")
			span.AddEvent("domain_throttle.backend_error", trace.WithAttributes(
				attribute.String("mail.domain", domain),
				attribute.String("domain_throttle.backend", d.cfg.DomainThrottleBackend),
				attribute.String("domain_throttle.stage", "observe"),
			))
		}
		d.metricInc("worker_delivery_success")
	}
	for _, rcpt := range msg.RcptTo {
		if ctx.Err() != nil && (d.cfg.MXShaping.Enabled || d.pauses != nil) {
			remaining = append(remaining, rcpt)
			deferredUntil = time.Now().Add(time.Second)
			continue
		}
		domain, _ := util.DomainOf(rcpt)
		checkPause := func() bool {
			if d.pauses != nil {
				entry, err := d.pauses.Lookup(ctx, domain)
				if err != nil || entry.Domain != "" {
					if d.cfg.MaxRetryAge > 0 && !msg.CreatedAt.IsZero() && time.Since(msg.CreatedAt) >= d.cfg.MaxRetryAge {
						d.emitHardBounceDSN(ctx, msg, rcpt, "delivery lifetime exceeded while paused")
						d.metricInc("worker_pause_expired")
						return true
					}
					remaining = append(remaining, rcpt)
					pausedRecipients[rcpt] = true
					until := time.Now().Add(30 * time.Second)
					if !entry.ExpiresAt.IsZero() && entry.ExpiresAt.Before(until) {
						until = entry.ExpiresAt
					}
					if until.After(deferredUntil) {
						deferredUntil = until
					}
					d.metricInc("worker_pause_deferred")
					if err != nil {
						d.metricInc("worker_pause_backend_error")
					}
					return true
				}
			}
			return false
		}
		if checkPause() {
			continue
		}
		batchKey, batchable := "", false
		if d.cl != nil {
			batchKey, batchable = d.cl.BatchKey(rcpt)
		}
		release := func() {}
		if d.throttle != nil && !(batchable && lmtpLeaseHeld[domain]) {
			var lease domainThrottleLease
			if d.cfg.MXShaping.Enabled {
				var wait time.Duration
				lease, wait = d.throttle.tryLease(ctx, domain)
				if wait > 0 {
					remaining = append(remaining, rcpt)
					until := time.Now().Add(wait)
					if until.After(deferredUntil) {
						deferredUntil = until
					}
					d.metricInc("worker_shaping_domain_deferred")
					if lease.backendError {
						d.metricInc("worker_domain_throttle_backend_error")
					}
					continue
				}
			} else {
				lease = d.throttle.acquire(domain)
			}
			release = lease.release
			if lease.waitDuration > 0 {
				waitMs := lease.waitDuration.Milliseconds()
				d.metricInc("worker_domain_throttle_acquire")
				d.metricAdd("worker_domain_throttle_wait_ms", uint64(waitMs))
				span.AddEvent("domain_throttle.acquire", trace.WithAttributes(
					attribute.String("mail.domain", domain),
					attribute.Int64("domain_throttle.wait_ms", waitMs),
				))
			}
			if lease.backendError {
				d.metricInc("worker_domain_throttle_backend_error")
				span.AddEvent("domain_throttle.backend_error", trace.WithAttributes(
					attribute.String("mail.domain", domain),
					attribute.String("domain_throttle.backend", d.cfg.DomainThrottleBackend),
					attribute.String("domain_throttle.stage", "acquire"),
				))
			}
		}
		func() {
			releaseAfter := true
			defer func() {
				if releaseAfter {
					release()
				}
			}()
			if checkPause() {
				return
			}
			if d.rep != nil {
				if ok, reason := d.rep.Admit(domain, time.Now().UTC()); !ok {
					reasons = append(reasons, rcpt+": reputation "+reason)
					d.metricInc("worker_reputation_block")
					switch reason {
					case "warmup_limit":
						d.metricInc("reputation_warmup_limit")
					case "bounce_rate":
						d.metricInc("reputation_bounce_rate_block")
					case "complaint_rate":
						d.metricInc("reputation_complaint_rate_block")
					}
					errs = append(errs, errors.New("reputation "+reason))
					failedRecipients[rcpt] = "reputation " + reason
					remaining = append(remaining, rcpt)
					return
				}
			}
			if d.sup != nil && d.sup.IsSuppressed(rcpt) {
				reasons = append(reasons, rcpt+": suppressed")
				d.metricInc("worker_suppressed_recipient")
				return
			}
			if batchable {
				lmtpBatches[batchKey] = append(lmtpBatches[batchKey], batchRecipient{rcpt: rcpt, domain: domain, release: release})
				if d.throttle != nil {
					lmtpLeaseHeld[domain] = true
				}
				releaseAfter = false
				return
			}
			handleResult(rcpt, domain, d.cl.Deliver(ctx, msg, rcpt))
		}()
	}
	for _, batch := range lmtpBatches {
		recipients := make([]string, 0, len(batch))
		for _, item := range batch {
			recipients = append(recipients, item.rcpt)
		}
		results := d.cl.DeliverBatch(ctx, msg, recipients)
		for _, item := range batch {
			handleResult(item.rcpt, item.domain, results[item.rcpt])
			item.release()
		}
	}
	persistRecipients := func(recipients []string, persist func() error) error {
		original := msg.RcptTo
		msg.RcptTo = recipients
		defer func() { msg.RcptTo = original }()
		return persist()
	}

	if len(errs) == 0 && !deferredUntil.IsZero() {
		span.SetAttributes(attribute.String("worker.result", "deferred"))
		if err := persistRecipients(remaining, func() error { return d.queue.Defer(msg, time.Until(deferredUntil)) }); err != nil {
			slog.ErrorContext(ctx, "shaping reschedule failed", "component", "worker", "msg_id", msg.ID, "error", err)
		}
		return
	}
	if len(errs) == 0 {
		span.SetAttributes(attribute.String("worker.result", "sent"))
		span.AddEvent("worker.ack_sent")
		if err := d.queue.AckSent(msg.ID, msg); err != nil {
			slog.ErrorContext(ctx, "ack sent failed", "component", "worker", "msg_id", msg.ID, "error", err)
		}
		d.metricInc("worker_ack_sent")
		return
	}

	reason := strings.Join(reasons, "; ")
	if shouldFail(msg, errs, d.cfg, time.Now().UTC()) {
		if len(pausedRecipients) > 0 {
			span.SetAttributes(attribute.String("worker.result", "deferred"))
			// A terminal failure for another recipient must not discard holds.
			held := []string{}
			for _, rcpt := range remaining {
				if diagnostic, failed := failedRecipients[rcpt]; failed {
					d.emitHardBounceDSN(ctx, msg, rcpt, diagnostic)
					continue
				}
				held = append(held, rcpt)
			}
			if err := persistRecipients(held, func() error { return d.queue.Defer(msg, time.Until(deferredUntil)) }); err != nil {
				slog.ErrorContext(ctx, "pause reschedule failed", "component", "worker", "msg_id", msg.ID, "error", err)
			}
			return
		}

		if joined := errors.Join(errs...); joined != nil {
			span.RecordError(joined)
		}
		span.SetAttributes(attribute.String("worker.result", "failed"))
		span.AddEvent("worker.failed", trace.WithAttributes(attribute.String("worker.reason", reason)))
		span.SetStatus(codes.Error, reason)
		if err := persistRecipients(remaining, func() error { return d.queue.Fail(msg, reason) }); err != nil {
			slog.ErrorContext(ctx, "mark failed failed", "component", "worker", "msg_id", msg.ID, "error", err)
		}
		d.metricInc("worker_mark_failed")
		return
	}
	delay := max(backoff(msg.Attempts, d.cfg.RetrySchedule), time.Until(deferredUntil))
	if joined := errors.Join(errs...); joined != nil {
		span.RecordError(joined)
	}
	span.SetAttributes(
		attribute.String("worker.result", "retry"),
		attribute.String("worker.retry_delay", delay.String()),
	)
	span.AddEvent("worker.retry_scheduled", trace.WithAttributes(
		attribute.String("worker.reason", reason),
		attribute.String("worker.retry_delay", delay.String()),
	))
	span.SetStatus(codes.Error, reason)
	// Retry only recipients that did not already complete on another route.
	if err := persistRecipients(remaining, func() error { return d.queue.Retry(msg, delay, reason) }); err != nil {
		slog.ErrorContext(ctx, "retry schedule failed", "component", "worker", "msg_id", msg.ID, "error", err)
	}
	d.metricInc("worker_retry_scheduled")
}

func (d *Dispatcher) emitHardBounceDSN(ctx context.Context, msg *model.Message, rcpt, diagnostic string) {
	if d.queue == nil {
		return
	}
	dsnMsg, err := bounce.BuildFailureDSN(msg, rcpt, diagnostic, d.cfg.Hostname, time.Now().UTC())
	if err != nil {
		return
	}
	observability.InjectTraceContext(ctx, dsnMsg)
	if err := d.queue.Enqueue(dsnMsg); err != nil {
		slog.ErrorContext(ctx, "enqueue hard dsn failed", "component", "worker", "msg_id", msg.ID, "rcpt", logging.MaskEmail(rcpt), "error", err)
	}
}

func (d *Dispatcher) emitSoftBounceDSN(ctx context.Context, msg *model.Message, rcpt, diagnostic string) {
	if d.queue == nil {
		return
	}
	// Send delayed notification once at first temporary failure to avoid excessive notices.
	if msg.Attempts > 0 {
		return
	}
	dsnMsg, err := bounce.BuildDelayDSN(msg, rcpt, diagnostic, d.cfg.Hostname, time.Now().UTC())
	if err != nil {
		return
	}
	observability.InjectTraceContext(ctx, dsnMsg)
	if err := d.queue.Enqueue(dsnMsg); err != nil {
		slog.ErrorContext(ctx, "enqueue soft dsn failed", "component", "worker", "msg_id", msg.ID, "rcpt", logging.MaskEmail(rcpt), "error", err)
	}
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

func (d *Dispatcher) metricAdd(name string, value uint64) {
	if d.m != nil {
		d.m.Counter(name).Add(value)
	}
}

// SetDeliveryPauses must be called before starting the dispatcher.
func (d *Dispatcher) SetDeliveryPauses(store deliverypause.Store) {
	d.pauses = store
	d.responses = newResponseThrottle(d.cfg.ResponseThrottle, d.pauses)
}

func (d *Dispatcher) refreshPauseStats(ctx context.Context) {
	if d.pauses == nil || d.m == nil {
		return
	}
	entries, err := d.pauses.List(ctx)
	if err != nil {
		d.metricInc("worker_pause_backend_error")
		return
	}
	d.m.SetGauge("delivery_pause_active_domains", int64(len(entries)))
	local := queue.LocalStore(d.queue)
	if local == nil {
		return
	}
	domains := map[string]bool{}
	for _, entry := range entries {
		domains[entry.Domain] = true
	}
	count, oldest, err := local.PauseStats(domains, time.Now())
	if err != nil {
		d.metricInc("worker_pause_stats_error")
		return
	}
	d.m.SetGauge("delivery_pause_messages", count)
	d.m.SetGauge("delivery_pause_oldest_age_seconds", int64(oldest.Seconds()))
}
