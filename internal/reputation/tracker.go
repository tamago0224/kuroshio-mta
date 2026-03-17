package reputation

import (
	"encoding/json"
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
	day            string
	todayAttempts  int
	successes      uint64
	temporaryFails uint64
	permanentFails uint64
	complaints     uint64
	tlsrptSuccess  uint64
	tlsrptFailure  uint64
}

type Tracker struct {
	cfg     Config
	mu      sync.Mutex
	domains map[string]*domainStats
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
	return &Tracker{cfg: cfg, domains: map[string]*domainStats{}}
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
	if domain == "" {
		return true, ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, now)
	limit := t.warmupLimit(now)
	if limit > 0 && st.todayAttempts >= limit {
		return false, "warmup_limit"
	}
	attempts := st.successes + st.temporaryFails + st.permanentFails
	if attempts >= uint64(t.cfg.MinSamples) {
		if float64(st.permanentFails)/float64(attempts) >= t.cfg.BounceThreshold {
			return false, "bounce_rate"
		}
		if float64(st.complaints)/float64(attempts) >= t.cfg.ComplaintThreshold {
			return false, "complaint_rate"
		}
	}
	st.todayAttempts++
	return true, ""
}

func (t *Tracker) ObserveDelivery(domain string, temporary, permanent bool) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, time.Now().UTC())
	switch {
	case permanent:
		st.permanentFails++
	case temporary:
		st.temporaryFails++
	default:
		st.successes++
	}
}

func (t *Tracker) RecordComplaint(domain string) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, time.Now().UTC())
	st.complaints++
}

func (t *Tracker) RecordTLSReport(domain string, success bool) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.getLocked(domain, time.Now().UTC())
	if success {
		st.tlsrptSuccess++
	} else {
		st.tlsrptFailure++
	}
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
	attempts := st.successes + st.temporaryFails + st.permanentFails
	s := DomainSnapshot{
		Domain:          domain,
		TodayAttempts:   st.todayAttempts,
		WarmupLimit:     t.warmupLimit(now),
		Successes:       st.successes,
		TemporaryFails:  st.temporaryFails,
		PermanentFails:  st.permanentFails,
		Complaints:      st.complaints,
		TLSRPTSuccesses: st.tlsrptSuccess,
		TLSRPTFailures:  st.tlsrptFailure,
	}
	if attempts > 0 {
		s.BounceRate = round4(float64(st.permanentFails) / float64(attempts))
		s.ComplaintRate = round4(float64(st.complaints) / float64(attempts))
	}
	if blocked, reason := t.evaluateBlocked(st, now); blocked {
		s.Blocked = true
		s.BlockReason = reason
	}
	return s
}

func (t *Tracker) evaluateBlocked(st *domainStats, now time.Time) (bool, string) {
	limit := t.warmupLimit(now)
	if limit > 0 && st.todayAttempts >= limit {
		return true, "warmup_limit"
	}
	attempts := st.successes + st.temporaryFails + st.permanentFails
	if attempts >= uint64(t.cfg.MinSamples) {
		if float64(st.permanentFails)/float64(attempts) >= t.cfg.BounceThreshold {
			return true, "bounce_rate"
		}
		if float64(st.complaints)/float64(attempts) >= t.cfg.ComplaintThreshold {
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
		st = &domainStats{}
		t.domains[domain] = st
	}
	day := now.UTC().Format("2006-01-02")
	if st.day != day {
		st.day = day
		st.todayAttempts = 0
	}
	return st
}

func normalizeDomain(in string) string {
	return strings.ToLower(strings.TrimSpace(in))
}

func round4(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}
