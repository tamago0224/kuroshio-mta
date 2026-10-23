package eventwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

type Dispatcher struct {
	cfg     config.EventWebhookConfig
	outbox  *Outbox
	secret  []byte
	client  *http.Client
	metrics *observability.Metrics
}

type deliveryError struct {
	reason string
	retry  bool
	delay  time.Duration
}

func (e *deliveryError) Error() string { return e.reason }

var errRedirect = errors.New("event webhook redirects are disabled")

func NewDispatcher(cfg config.EventWebhookConfig, outbox *Outbox, metrics *observability.Metrics) (*Dispatcher, error) {
	secret, err := os.ReadFile(cfg.SecretFile)
	if err != nil {
		return nil, fmt.Errorf("read event webhook secret: %w", err)
	}
	secret = bytes.TrimRight(secret, "\r\n")
	if len(secret) < 32 || len(secret) > 4096 || bytes.ContainsAny(secret, "\r\n") {
		return nil, errors.New("event webhook secret must be a 32..4096 byte single line")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect }}
	return &Dispatcher{cfg: cfg, outbox: outbox, secret: secret, client: client, metrics: metrics}, nil
}

func (d *Dispatcher) Close() { d.client.CloseIdleConnections() }

func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			d.dispatch(ctx)
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) {
	now := time.Now().UTC()
	records, err := d.outbox.due(now, 100)
	if err != nil {
		d.metric("event_webhook_store_errors")
		slog.ErrorContext(ctx, "event webhook outbox read failed", "component", "event_webhook", "error", err)
		return
	}
	for _, r := range records {
		if ctx.Err() != nil {
			return
		}
		err := d.send(ctx, r.Event)
		if err == nil {
			if err := d.outbox.ack(r.EventID); err != nil {
				d.metric("event_webhook_store_errors")
			} else {
				d.metric("event_webhook_delivered")
			}
			continue
		}
		var deliveryErr *deliveryError
		retry := errors.As(err, &deliveryErr) && deliveryErr.retry && r.Attempts+1 < d.cfg.MaxAttempts
		if retry {
			delay := d.backoff(r.Attempts)
			if deliveryErr.delay > delay {
				delay = min(deliveryErr.delay, d.cfg.MaxRetryDelay)
			}
			if storeErr := d.outbox.retry(r, delay, err.Error()); storeErr != nil {
				d.metric("event_webhook_store_errors")
			} else {
				d.metric("event_webhook_retried")
			}
		} else if storeErr := d.outbox.fail(r, err.Error()); storeErr != nil {
			d.metric("event_webhook_store_errors")
		} else {
			d.metric("event_webhook_dlq")
		}
	}
	if err := d.outbox.sweep(now, d.cfg.Retention); err != nil {
		d.metric("event_webhook_store_errors")
	}
}

func (d *Dispatcher) send(ctx context.Context, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, d.secret)
	_, _ = mac.Write(body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kuroshio-Event-ID", event.EventID)
	req.Header.Set("X-Kuroshio-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := d.client.Do(req)
	if err != nil {
		if errors.Is(err, errRedirect) {
			return &deliveryError{reason: "webhook redirect rejected"}
		}
		return &deliveryError{reason: "webhook request failed", retry: true}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return &deliveryError{reason: "webhook returned 429", retry: true, delay: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
	case resp.StatusCode >= 500:
		return &deliveryError{reason: fmt.Sprintf("webhook returned %d", resp.StatusCode), retry: true}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &deliveryError{reason: fmt.Sprintf("webhook authentication failed with %d", resp.StatusCode)}
	default:
		return &deliveryError{reason: fmt.Sprintf("webhook returned terminal status %d", resp.StatusCode)}
	}
}

func retryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}
func (d *Dispatcher) backoff(attempt int) time.Duration {
	delay := time.Second << min(attempt, 20)
	return min(delay, d.cfg.MaxRetryDelay)
}
func (d *Dispatcher) metric(name string) {
	if d.metrics != nil {
		d.metrics.Counter(name).Inc()
	}
}
