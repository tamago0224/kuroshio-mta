package ingress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type RateRule struct {
	Event  string
	Key    string
	Limit  int
	Window time.Duration
}

func ParseRateRules(raw string) ([]RateRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	chunks := strings.Split(raw, ";")
	rules := make([]RateRule, 0, len(chunks))
	for _, ch := range chunks {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		parts := strings.Split(ch, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid rule format: %q", ch)
		}
		event := strings.ToLower(strings.TrimSpace(parts[0]))
		key := strings.ToLower(strings.TrimSpace(parts[1]))
		limit, err := parseInt(parts[2])
		if err != nil || limit <= 0 {
			return nil, fmt.Errorf("invalid limit in rule: %q", ch)
		}
		window, err := time.ParseDuration(strings.TrimSpace(parts[3]))
		if err != nil || window <= 0 {
			return nil, fmt.Errorf("invalid window in rule: %q", ch)
		}
		if event != "connect" && event != "helo" && event != "mailfrom" {
			return nil, fmt.Errorf("invalid event in rule: %q", ch)
		}
		if key != "ip" && key != "helo" && key != "mailfrom" && key != "ip+helo" && key != "ip+mailfrom" {
			return nil, fmt.Errorf("invalid key in rule: %q", ch)
		}
		rules = append(rules, RateRule{Event: event, Key: key, Limit: limit, Window: window})
	}
	return rules, nil
}

type FlexibleLimiter struct {
	rules   []RateRule
	mu      sync.Mutex
	buckets map[int]*RateLimiter
}

func NewFlexibleLimiter(rules []RateRule) *FlexibleLimiter {
	if len(rules) == 0 {
		return nil
	}
	return &FlexibleLimiter{rules: append([]RateRule(nil), rules...), buckets: map[int]*RateLimiter{}}
}

func (f *FlexibleLimiter) Allow(event, ip, helo, mailFrom string, now time.Time) bool {
	if f == nil {
		return true
	}
	for i, r := range f.rules {
		if r.Event != event {
			continue
		}
		key := buildRateKey(r.Key, ip, helo, mailFrom)
		if key == "" {
			continue
		}
		lim := f.bucket(i, r)
		if !lim.Allow(key, now) {
			return false
		}
	}
	return true
}

func (f *FlexibleLimiter) bucket(idx int, r RateRule) *RateLimiter {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.buckets[idx]; ok {
		return b
	}
	b := NewRateLimiter(r.Limit, r.Window)
	f.buckets[idx] = b
	return b
}

func buildRateKey(keyType, ip, helo, mailFrom string) string {
	ip = strings.ToLower(strings.TrimSpace(ip))
	helo = strings.ToLower(strings.TrimSpace(helo))
	mailFrom = strings.ToLower(strings.TrimSpace(mailFrom))
	switch keyType {
	case "ip":
		return ip
	case "helo":
		return helo
	case "mailfrom":
		return mailFrom
	case "ip+helo":
		return ip + "|" + helo
	case "ip+mailfrom":
		return ip + "|" + mailFrom
	default:
		return ""
	}
}

func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("invalid int")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}
