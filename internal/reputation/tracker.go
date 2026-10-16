package reputation

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WarmupStep struct {
	AfterDays int `json:"after_days"`
	Limit     int `json:"limit"`
}

type Config struct {
	StartDate          time.Time
	WarmupRules        []WarmupStep
	BounceThreshold    float64
	ComplaintThreshold float64
	MinSamples         int
}

type DomainSnapshot struct {
	Domain          string  `json:"domain"`
	TodayAttempts   int     `json:"today_attempts"`
	WarmupLimit     int     `json:"warmup_limit"`
	Successes       uint64  `json:"successes"`
	TemporaryFails  uint64  `json:"temporary_fails"`
	PermanentFails  uint64  `json:"permanent_fails"`
	Complaints      uint64  `json:"complaints"`
	TLSRPTSuccesses uint64  `json:"tlsrpt_successes"`
	TLSRPTFailures  uint64  `json:"tlsrpt_failures"`
	BounceRate      float64 `json:"bounce_rate"`
	ComplaintRate   float64 `json:"complaint_rate"`
	Blocked         bool    `json:"blocked"`
	BlockReason     string  `json:"block_reason,omitempty"`
}

type domainStats struct {
	Day            string
	TodayAttempts  int
	Successes      uint64
	TemporaryFails uint64
	PermanentFails uint64
	Complaints     uint64
	TLSRPTSuccess  uint64
	TLSRPTFailure  uint64
}

type Tracker struct {
	cfg       Config
	mu        sync.Mutex
	domains   map[string]*domainStats
	statePath string
	seen      map[string]time.Time
	dirty     bool
	ingestion map[string]uint64
}

func New(cfg Config) *Tracker {
	if cfg.BounceThreshold <= 0 || cfg.BounceThreshold >= 1 {
		cfg.BounceThreshold = 0.05
	}
	if cfg.ComplaintThreshold <= 0 || cfg.ComplaintThreshold >= 1 {
		cfg.ComplaintThreshold = 0.001
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 100
	}
	if len(cfg.WarmupRules) == 0 {
		cfg.WarmupRules = []WarmupStep{
			{AfterDays: 0, Limit: 100},
			{AfterDays: 7, Limit: 1000},
			{AfterDays: 14, Limit: 5000},
		}
	}
	sort.Slice(cfg.WarmupRules, func(i, j int) bool {
		return cfg.WarmupRules[i].AfterDays < cfg.WarmupRules[j].AfterDays
	})
	return &Tracker{cfg: cfg, domains: map[string]*domainStats{}, seen: map[string]time.Time{}, ingestion: map[string]uint64{}}
}

func ParseWarmupRules(raw string) []WarmupStep {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]WarmupStep, 0, len(parts))
	for _, part := range parts {
		pair := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(pair) != 2 {
			continue
		}
		afterDays, err1 := strconv.Atoi(strings.TrimSpace(pair[0]))
		limit, err2 := strconv.Atoi(strings.TrimSpace(pair[1]))
		if err1 != nil || err2 != nil || afterDays < 0 || limit <= 0 {
			continue
		}
		out = append(out, WarmupStep{AfterDays: afterDays, Limit: limit})
	}
	return out
}

func ParseStartDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func (t *Tracker) Admit(domain string, now time.Time) (bool, string) {
	domain = normalizeDomain(domain)
	if !validReportDomain(domain) {
		return true, ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, now)
	limit := t.warmupLimit(now)
	if limit > 0 && st.TodayAttempts >= limit {
		return false, "warmup_limit"
	}
	attempts := st.Successes + st.TemporaryFails + st.PermanentFails
	if attempts >= uint64(t.cfg.MinSamples) {
		if float64(st.PermanentFails)/float64(attempts) >= t.cfg.BounceThreshold {
			return false, "bounce_rate"
		}
		if float64(st.Complaints)/float64(attempts) >= t.cfg.ComplaintThreshold {
			return false, "complaint_rate"
		}
	}
	st.TodayAttempts++
	t.dirty = true
	return true, ""
}

func (t *Tracker) ObserveDelivery(domain string, temporary, permanent bool) {
	domain = normalizeDomain(domain)
	if !validReportDomain(domain) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, time.Now().UTC())
	t.dirty = true
	switch {
	case permanent:
		st.PermanentFails++
	case temporary:
		st.TemporaryFails++
	default:
		st.Successes++
	}
}

func (t *Tracker) RecordComplaint(domain string) error {
	return t.record(domain, func(stats *domainStats) error {
		if stats.Complaints == ^uint64(0) {
			return errors.New("complaint count overflow")
		}
		stats.Complaints++
		return nil
	})
}

func (t *Tracker) RecordTLSReport(domain string, success bool) error {
	return t.record(domain, func(stats *domainStats) error {
		count := &stats.TLSRPTFailure
		if success {
			count = &stats.TLSRPTSuccess
		}
		if *count == ^uint64(0) {
			return errors.New("TLS report count overflow")
		}
		*count = *count + 1
		return nil
	})
}

func (t *Tracker) record(domain string, update func(*domainStats) error) error {
	domain = normalizeDomain(domain)
	if !validReportDomain(domain) {
		return ErrInvalidDomain
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	previous, exists := t.domains[domain]
	var saved domainStats
	if exists {
		saved = *previous
	}
	stats := t.getLocked(domain, time.Now().UTC())
	err := update(stats)
	if err == nil {
		t.dirty = true
		err = t.flushLocked()
	}
	if err != nil {
		if exists {
			t.domains[domain] = &saved
		} else {
			delete(t.domains, domain)
		}
	}
	return err
}

func (t *Tracker) Snapshot(now time.Time) []DomainSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]DomainSnapshot, 0, len(t.domains))
	for domain := range t.domains {
		out = append(out, t.snapshotLocked(domain, now))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Domain < out[j].Domain
	})
	return out
}

func (t *Tracker) JSON(now time.Time) []byte {
	b, _ := json.Marshal(t.Snapshot(now))
	return b
}

func (t *Tracker) snapshotLocked(domain string, now time.Time) DomainSnapshot {
	st := t.getLocked(domain, now)
	attempts := st.Successes + st.TemporaryFails + st.PermanentFails
	s := DomainSnapshot{
		Domain:          domain,
		TodayAttempts:   st.TodayAttempts,
		WarmupLimit:     t.warmupLimit(now),
		Successes:       st.Successes,
		TemporaryFails:  st.TemporaryFails,
		PermanentFails:  st.PermanentFails,
		Complaints:      st.Complaints,
		TLSRPTSuccesses: st.TLSRPTSuccess,
		TLSRPTFailures:  st.TLSRPTFailure,
	}
	if attempts > 0 {
		s.BounceRate = round4(float64(st.PermanentFails) / float64(attempts))
		s.ComplaintRate = round4(float64(st.Complaints) / float64(attempts))
	}
	if blocked, reason := t.evaluateBlocked(st, now); blocked {
		s.Blocked = true
		s.BlockReason = reason
	}
	return s
}

func (t *Tracker) evaluateBlocked(st *domainStats, now time.Time) (bool, string) {
	limit := t.warmupLimit(now)
	if limit > 0 && st.TodayAttempts >= limit {
		return true, "warmup_limit"
	}
	attempts := st.Successes + st.TemporaryFails + st.PermanentFails
	if attempts >= uint64(t.cfg.MinSamples) {
		if float64(st.PermanentFails)/float64(attempts) >= t.cfg.BounceThreshold {
			return true, "bounce_rate"
		}
		if float64(st.Complaints)/float64(attempts) >= t.cfg.ComplaintThreshold {
			return true, "complaint_rate"
		}
	}
	return false, ""
}

func (t *Tracker) warmupLimit(now time.Time) int {
	if t.cfg.StartDate.IsZero() {
		return 0
	}
	days := int(now.UTC().Sub(t.cfg.StartDate.UTC()) / (24 * time.Hour))
	limit := 0
	for _, step := range t.cfg.WarmupRules {
		if days >= step.AfterDays {
			limit = step.Limit
		}
	}
	return limit
}

func (t *Tracker) getLocked(domain string, now time.Time) *domainStats {
	st, ok := t.domains[domain]
	if !ok {
		t.dirty = true
		st = &domainStats{}
		t.domains[domain] = st
	}
	day := now.UTC().Format("2006-01-02")
	if st.Day != day {
		t.dirty = true
		st.Day = day
		st.TodayAttempts = 0
	}
	return st
}

func normalizeDomain(in string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in)), ".")
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
