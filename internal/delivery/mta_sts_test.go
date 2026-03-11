package delivery

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
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

func TestParseMTASTSPolicyID(t *testing.T) {
	id, ok := parseMTASTSPolicyID([]string{
		"v=STSv1; id=20260311T120000",
	})
	if !ok {
		t.Fatal("expected id to be parsed")
	}
	if id != "20260311T120000" {
		t.Fatalf("unexpected id: %q", id)
	}
}

func TestMTASTSResolverRefreshesPolicyWhenTXTIDChanges(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	fetchCalls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		fetchCalls++
		return "version: STSv1\nmode: enforce\nmx: mx2.example.net\nmax_age: 120\n", nil
	})
	r.nowFn = func() time.Time { return now }
	r.lookupTXT = func(context.Context, string) ([]string, error) {
		return []string{"v=STSv1; id=new-id"}, nil
	}
	r.cache["example.com"] = MTASTSPolicy{
		Version:   "STSv1",
		Mode:      "enforce",
		MX:        []string{"mx1.example.net"},
		MaxAge:    time.Hour,
		ExpiresAt: now.Add(time.Hour),
		PolicyID:  "old-id",
	}

	p1, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup1: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected refresh fetch due to id mismatch, calls=%d", fetchCalls)
	}
	if len(p1.MX) != 1 || p1.MX[0] != "mx1.example.net" {
		t.Fatalf("expected previous policy at first rollover observation, got %+v", p1)
	}

	p2, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup2: %v", err)
	}
	if fetchCalls != 2 {
		t.Fatalf("expected second fetch confirmation, calls=%d", fetchCalls)
	}
	if len(p2.MX) != 1 || p2.MX[0] != "mx2.example.net" {
		t.Fatalf("expected refreshed policy after confirmation, got %+v", p2)
	}
	if p2.PolicyID != "new-id" {
		t.Fatalf("expected policy id to update, got %q", p2.PolicyID)
	}
}

func TestMTASTSResolverKeepsCachedPolicyWhenTXTLookupFails(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	fetchCalls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		fetchCalls++
		return "version: STSv1\nmode: enforce\nmx: mx2.example.net\nmax_age: 120\n", nil
	})
	r.nowFn = func() time.Time { return now }
	r.lookupTXT = func(context.Context, string) ([]string, error) {
		return nil, errors.New("dns failed")
	}
	r.cache["example.com"] = MTASTSPolicy{
		Version:   "STSv1",
		Mode:      "enforce",
		MX:        []string{"mx1.example.net"},
		MaxAge:    time.Hour,
		ExpiresAt: now.Add(time.Hour),
		PolicyID:  "old-id",
	}

	p, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fetchCalls != 0 {
		t.Fatalf("expected no refresh fetch when txt lookup fails, calls=%d", fetchCalls)
	}
	if len(p.MX) != 1 || p.MX[0] != "mx1.example.net" {
		t.Fatalf("expected cached policy, got %+v", p)
	}
}

func TestMTASTSResolverSafeRolloverRequiresTwoConsistentFetches(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	fetchCalls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		fetchCalls++
		return "version: STSv1\nmode: enforce\nmx: mx-new.example.net\nmax_age: 120\n", nil
	})
	r.nowFn = func() time.Time { return now }
	r.lookupTXT = func(context.Context, string) ([]string, error) {
		return []string{"v=STSv1; id=new-id"}, nil
	}
	r.cache["example.com"] = MTASTSPolicy{
		Version:   "STSv1",
		Mode:      "enforce",
		MX:        []string{"mx-old.example.net"},
		MaxAge:    time.Hour,
		ExpiresAt: now.Add(time.Hour),
		PolicyID:  "old-id",
	}

	p1, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup1: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected first fetch, calls=%d", fetchCalls)
	}
	if len(p1.MX) != 1 || p1.MX[0] != "mx-old.example.net" {
		t.Fatalf("expected old policy on first observed rollover, got %+v", p1)
	}

	p2, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup2: %v", err)
	}
	if fetchCalls != 2 {
		t.Fatalf("expected second fetch confirmation, calls=%d", fetchCalls)
	}
	if len(p2.MX) != 1 || p2.MX[0] != "mx-new.example.net" {
		t.Fatalf("expected new policy after confirmation, got %+v", p2)
	}
	if p2.PolicyID != "new-id" {
		t.Fatalf("expected switched policy id, got %q", p2.PolicyID)
	}
}

func TestMTASTSResolverSafeRolloverAppliesImmediatelyWithoutPreviousPolicy(t *testing.T) {
	now := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	fetchCalls := 0
	r := NewMTASTSResolver(5*time.Minute, 2*time.Second, func(ctx context.Context, domain string) (string, error) {
		fetchCalls++
		return "version: STSv1\nmode: enforce\nmx: mx-new.example.net\nmax_age: 120\n", nil
	})
	r.nowFn = func() time.Time { return now }
	r.lookupTXT = func(context.Context, string) ([]string, error) {
		return []string{"v=STSv1; id=new-id"}, nil
	}

	p, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected single fetch, calls=%d", fetchCalls)
	}
	if len(p.MX) != 1 || p.MX[0] != "mx-new.example.net" {
		t.Fatalf("expected immediate apply when no previous policy, got %+v", p)
	}
}

func TestNewMTASTSHTTPClientSetsStrictTLSAndNoRedirect(t *testing.T) {
	cl := newMTASTSHTTPClient(3 * time.Second)
	if cl.Timeout != 3*time.Second {
		t.Fatalf("unexpected timeout: %v", cl.Timeout)
	}
	if cl.CheckRedirect == nil {
		t.Fatal("expected redirect policy to be set")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := cl.CheckRedirect(req, []*http.Request{req}); err == nil {
		t.Fatal("expected redirect to be rejected")
	}
	tr, ok := cl.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type: %T", cl.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("expected tls config")
	}
	if tr.TLSClientConfig.MinVersion != 0x0303 {
		t.Fatalf("expected TLS1.2 min version, got %x", tr.TLSClientConfig.MinVersion)
	}
}

func TestIsMTASTSCertificateValidationError(t *testing.T) {
	if !isMTASTSCertificateValidationError(&x509.UnknownAuthorityError{}) {
		t.Fatal("expected unknown authority as cert validation error")
	}
	if !isMTASTSCertificateValidationError(&x509.HostnameError{}) {
		t.Fatal("expected hostname error as cert validation error")
	}
	if !isMTASTSCertificateValidationError(&x509.CertificateInvalidError{}) {
		t.Fatal("expected certificate invalid error as cert validation error")
	}
	uErr := &url.Error{Err: &x509.UnknownAuthorityError{}}
	if !isMTASTSCertificateValidationError(uErr) {
		t.Fatal("expected wrapped x509 error to be detected")
	}
	if isMTASTSCertificateValidationError(errors.New("timeout")) {
		t.Fatal("did not expect generic error to be cert validation error")
	}
}
