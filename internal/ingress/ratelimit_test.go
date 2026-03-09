package ingress

import (
	"testing"
	"time"
)

func TestRateLimiterAllowWithinLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	now := time.Unix(1000, 0)
	if !rl.Allow("1.2.3.4", now) {
		t.Fatal("1st request should pass")
	}
	if !rl.Allow("1.2.3.4", now.Add(10*time.Second)) {
		t.Fatal("2nd request should pass")
	}
	if !rl.Allow("1.2.3.4", now.Add(20*time.Second)) {
		t.Fatal("3rd request should pass")
	}
	if rl.Allow("1.2.3.4", now.Add(30*time.Second)) {
		t.Fatal("4th request in window should be limited")
	}
}

func TestRateLimiterExpiresWindow(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	now := time.Unix(1000, 0)
	_ = rl.Allow("1.2.3.4", now)
	_ = rl.Allow("1.2.3.4", now.Add(10*time.Second))
	if rl.Allow("1.2.3.4", now.Add(20*time.Second)) {
		t.Fatal("should be rate limited")
	}
	if !rl.Allow("1.2.3.4", now.Add(61*time.Second)) {
		t.Fatal("should recover after window")
	}
}
