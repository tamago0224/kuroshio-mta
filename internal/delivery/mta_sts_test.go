package delivery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseMTASTSPolicy(t *testing.T) {
	raw := "version: STSv1\nmode: enforce\nmx: *.example.net\nmx: mail.example.org\nmax_age: 86400\n"
	p, err := parseMTASTSPolicy(raw)
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	if p.Mode != "enforce" || p.MaxAge != 24*time.Hour {
		t.Fatalf("unexpected policy: %+v", p)
	}
	if len(p.MX) != 2 {
		t.Fatalf("mx count=%d", len(p.MX))
	}
}

func TestMTASTSAllowsMX(t *testing.T) {
	p := MTASTSPolicy{Mode: "enforce", MX: []string{"*.example.net", "mail.example.org"}}
	if !p.AllowsMX("mx1.example.net") {
		t.Fatal("wildcard should match")
	}
	if !p.AllowsMX("mail.example.org") {
		t.Fatal("exact match should match")
	}
	if p.AllowsMX("bad.example.com") {
		t.Fatal("unlisted host should not match")
	}
}

func TestMTASTSResolverCachesResult(t *testing.T) {
	calls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		calls++
		return "version: STSv1\nmode: enforce\nmx: *.example.net\nmax_age: 120\n", nil
	})
	p1, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup1: %v", err)
	}
	p2, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup2: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected cache hit, calls=%d", calls)
	}
	if p1.Mode != p2.Mode {
		t.Fatalf("cached policy mismatch")
	}
}

func TestMTASTSResolverReturnsStaleOnFetchFailure(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	calls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		calls++
		return "", errors.New("fetch failed")
	})
	r.nowFn = func() time.Time { return now }
	r.cache["example.com"] = MTASTSPolicy{
		Version:   "STSv1",
		Mode:      "enforce",
		MX:        []string{"mx.example.net"},
		MaxAge:    time.Hour,
		ExpiresAt: now.Add(-time.Minute),
	}

	p, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup should return stale policy on fetch failure: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one fetch attempt, calls=%d", calls)
	}
	if p.Mode != "enforce" || len(p.MX) != 1 || p.MX[0] != "mx.example.net" {
		t.Fatalf("unexpected stale policy: %+v", p)
	}
}

func TestMTASTSResolverUsesCooldownBeforeRetry(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	calls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		calls++
		return "", errors.New("fetch failed")
	})
	r.nowFn = func() time.Time { return now }
	r.minRetryDelay = 10 * time.Second
	r.maxRetryDelay = 10 * time.Second
	r.cache["example.com"] = MTASTSPolicy{
		Version:   "STSv1",
		Mode:      "enforce",
		MX:        []string{"mx.example.net"},
		MaxAge:    time.Hour,
		ExpiresAt: now.Add(-time.Minute),
	}

	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected first fetch attempt, calls=%d", calls)
	}

	now = now.Add(5 * time.Second)
	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatalf("second lookup during cooldown: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected no fetch during cooldown, calls=%d", calls)
	}

	now = now.Add(6 * time.Second)
	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatalf("third lookup after cooldown: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected retry fetch after cooldown, calls=%d", calls)
	}
}
