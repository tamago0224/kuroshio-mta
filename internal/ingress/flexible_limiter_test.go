package ingress

import (
	"testing"
	"time"
)

func TestParseRateRules(t *testing.T) {
	raw := "connect:ip:100:1m;helo:ip+helo:20:1m;mailfrom:ip+mailfrom:10:1m"
	rules, err := ParseRateRules(raw)
	if err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("len=%d", len(rules))
	}
}

func TestFlexibleLimiter(t *testing.T) {
	rules := []RateRule{{Event: "connect", Key: "ip", Limit: 1, Window: time.Minute}, {Event: "mailfrom", Key: "ip+mailfrom", Limit: 1, Window: time.Minute}}
	l := NewFlexibleLimiter(rules)
	now := time.Unix(1000, 0)

	if !l.Allow("connect", "1.2.3.4", "", "", now) {
		t.Fatal("first connect should pass")
	}
	if l.Allow("connect", "1.2.3.4", "", "", now.Add(time.Second)) {
		t.Fatal("second connect should be blocked")
	}

	if !l.Allow("mailfrom", "1.2.3.4", "ehlo", "a@example.com", now) {
		t.Fatal("first mailfrom should pass")
	}
	if l.Allow("mailfrom", "1.2.3.4", "ehlo", "a@example.com", now.Add(time.Second)) {
		t.Fatal("second same mailfrom should be blocked")
	}
	if !l.Allow("mailfrom", "1.2.3.4", "ehlo", "b@example.com", now.Add(time.Second)) {
		t.Fatal("different mailfrom should pass")
	}
}
