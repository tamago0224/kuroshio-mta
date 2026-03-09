package delivery

import (
	"context"
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
