package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
)

const maxResponseThrottleKeys = 10000

var errResponseThrottleCapacity = errors.New("response throttle tracking capacity reached")

type responseThrottle struct {
	cfg    config.ResponseThrottleConfig
	store  deliverypause.Store
	mu     sync.Mutex
	events map[string][]time.Time
	now    func() time.Time
}

func newResponseThrottle(cfg config.ResponseThrottleConfig, store deliverypause.Store) *responseThrottle {
	if !cfg.Enabled || store == nil && !cfg.DryRun {
		return nil
	}
	return &responseThrottle{cfg: cfg, store: store, events: map[string][]time.Time{}, now: time.Now}
}

func (r *responseThrottle) observe(ctx context.Context, domain string, err error) (string, bool, error) {
	var smtpErr *delivery.SMTPResponseError
	if !errors.As(err, &smtpErr) || !smtpErr.Temporary() {
		return "", false, nil
	}
	classification := delivery.ClassifyFailure(err)
	now := r.now().UTC()
	for _, rule := range r.cfg.Rules {
		if !responseRuleMatches(rule, domain, smtpErr.Code, string(classification.Stage), classification.EnhancedStatus) {
			continue
		}
		key := rule.ID + "\x00" + domain
		r.mu.Lock()
		if _, exists := r.events[key]; !exists && len(r.events) >= maxResponseThrottleKeys {
			r.mu.Unlock()
			return rule.ID, false, errResponseThrottleCapacity
		}
		values := r.events[key][:0]
		for _, at := range r.events[key] {
			if at.After(now.Add(-rule.Window)) {
				values = append(values, at)
			}
		}
		values = append(values, now)
		r.events[key] = values
		triggered := len(values) >= rule.Threshold
		if triggered {
			delete(r.events, key)
		}
		r.mu.Unlock()
		if !triggered {
			return rule.ID, false, nil
		}
		if r.cfg.DryRun {
			return rule.ID, true, nil
		}
		existing, lookupErr := r.store.Lookup(ctx, domain)
		if lookupErr != nil {
			return rule.ID, true, lookupErr
		}
		if existing.Domain != "" {
			return rule.ID, true, nil
		}
		reason := fmt.Sprintf("automatic response throttle rule %s", rule.ID)
		entry := deliverypause.Entry{Domain: domain, Reason: reason, Actor: "auto:response-throttle:" + rule.ID, ExpiresAt: now.Add(rule.Pause)}
		return rule.ID, true, r.store.Put(ctx, entry, false)
	}
	return "", false, nil
}

func responseRuleMatches(rule config.ResponseThrottleRule, domain string, code int, stage, enhanced string) bool {
	if rule.SMTPCode != code || rule.Stage != "" && !strings.EqualFold(rule.Stage, stage) || rule.EnhancedStatus != "" && rule.EnhancedStatus != enhanced {
		return false
	}
	if len(rule.Domains) == 0 {
		return true
	}
	for _, candidate := range rule.Domains {
		if candidate == "*" || strings.EqualFold(strings.TrimSuffix(candidate, "."), domain) {
			return true
		}
	}
	return false
}
