package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/router"
)

func TestDeliverByMX_DANETakesPrecedenceOverMTASTS(t *testing.T) {
	cl := NewClient(config.Config{})
	cl.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
		return []router.MXHost{{Host: "mx1.example.net", Pref: 10}, {Host: "mx2.example.net", Pref: 20}}, nil
	}
	cl.mtaSTS = NewMTASTSResolver(time.Minute, time.Second, func(context.Context, string) (string, error) {
		return "version: STSv1\nmode: enforce\nmx: blocked.example.net\nmax_age: 3600\n", nil
	})
	cl.dane = NewDANEResolver(time.Second, func(_ context.Context, host string, _ int) (DANEResult, error) {
		if host != "mx1.example.net" {
			return DANEResult{}, nil
		}
		return DANEResult{
			AuthenticatedData: true,
			Records:           []TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0x01}}},
		}, nil
	})

	var calledHost string
	var requireTLS bool
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, reqTLS bool) error {
		calledHost = host
		requireTLS = reqTLS
		return nil
	}

	err := cl.deliverByMX(context.Background(), &model.Message{MailFrom: "sender@example.org", Data: []byte("x")}, "user@example.org")
	if err != nil {
		t.Fatalf("deliverByMX: %v", err)
	}
	if calledHost != "mx1.example.net" {
		t.Fatalf("expected DANE path to keep mx1 candidate, got %q", calledHost)
	}
	if !requireTLS {
		t.Fatal("expected TLS required when DANE is active")
	}
}

func TestDeliverByMX_FallsBackToMTASTSWhenNoUsableDANE(t *testing.T) {
	cl := NewClient(config.Config{})
	cl.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
		return []router.MXHost{{Host: "mx1.example.net", Pref: 10}, {Host: "mx2.example.net", Pref: 20}}, nil
	}
	cl.mtaSTS = NewMTASTSResolver(time.Minute, time.Second, func(context.Context, string) (string, error) {
		return "version: STSv1\nmode: enforce\nmx: mx2.example.net\nmax_age: 3600\n", nil
	})
	cl.dane = NewDANEResolver(time.Second, func(context.Context, string, int) (DANEResult, error) {
		return DANEResult{}, nil
	})

	var calledHost string
	var requireTLS bool
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, reqTLS bool) error {
		calledHost = host
		requireTLS = reqTLS
		return nil
	}

	err := cl.deliverByMX(context.Background(), &model.Message{MailFrom: "sender@example.org", Data: []byte("x")}, "user@example.org")
	if err != nil {
		t.Fatalf("deliverByMX: %v", err)
	}
	if calledHost != "mx2.example.net" {
		t.Fatalf("expected MTA-STS filtering to pick mx2, got %q", calledHost)
	}
	if !requireTLS {
		t.Fatal("expected TLS required by MTA-STS enforce")
	}
}

func TestDeliverByMX_MTASTSTestingModeDoesNotRejectOnMXMismatch(t *testing.T) {
	cl := NewClient(config.Config{})
	cl.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
		return []router.MXHost{{Host: "mx1.example.net", Pref: 10}, {Host: "mx2.example.net", Pref: 20}}, nil
	}
	cl.mtaSTS = NewMTASTSResolver(time.Minute, time.Second, func(context.Context, string) (string, error) {
		return "version: STSv1\nmode: testing\nmx: mx2.example.net\nmax_age: 3600\n", nil
	})

	var calledHost string
	var requireTLS bool
	var violationCalled bool
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, reqTLS bool) error {
		calledHost = host
		requireTLS = reqTLS
		return nil
	}
	cl.reportMTASTSTestingViolationFn = func(context.Context, string, string, string) {
		violationCalled = true
	}

	err := cl.deliverByMX(context.Background(), &model.Message{MailFrom: "sender@example.org", Data: []byte("x")}, "user@example.org")
	if err != nil {
		t.Fatalf("deliverByMX: %v", err)
	}
	if calledHost != "mx1.example.net" {
		t.Fatalf("expected first MX host to be used without hard enforcement, got %q", calledHost)
	}
	if requireTLS {
		t.Fatal("testing mode must not require TLS")
	}
	if !violationCalled {
		t.Fatal("expected testing mode violation report on mx mismatch")
	}
}

func TestDeliverByMX_MTASTSTestingModeNoViolationWhenMXMatches(t *testing.T) {
	cl := NewClient(config.Config{})
	cl.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
		return []router.MXHost{{Host: "mx1.example.net", Pref: 10}}, nil
	}
	cl.mtaSTS = NewMTASTSResolver(time.Minute, time.Second, func(context.Context, string) (string, error) {
		return "version: STSv1\nmode: testing\nmx: mx1.example.net\nmax_age: 3600\n", nil
	})

	var violationCalled bool
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, reqTLS bool) error {
		return nil
	}
	cl.reportMTASTSTestingViolationFn = func(context.Context, string, string, string) {
		violationCalled = true
	}

	err := cl.deliverByMX(context.Background(), &model.Message{MailFrom: "sender@example.org", Data: []byte("x")}, "user@example.org")
	if err != nil {
		t.Fatalf("deliverByMX: %v", err)
	}
	if violationCalled {
		t.Fatal("did not expect testing mode violation report when mx matches policy")
	}
}

func TestDeliverByMX_DANETrustModelAllowsUnsignedWhenConfigured(t *testing.T) {
	cl := NewClient(config.Config{DANEDNSSECTrustModel: "insecure_allow_unsigned"})
	cl.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
		return []router.MXHost{{Host: "mx1.example.net", Pref: 10}}, nil
	}
	cl.dane = NewDANEResolver(time.Second, func(_ context.Context, host string, _ int) (DANEResult, error) {
		return DANEResult{
			AuthenticatedData: false,
			Records:           []TLSARecord{{Usage: 3, Selector: 1, MatchingType: 1, CertificateAssociation: []byte{0x01}}},
		}, nil
	})

	var requireTLS bool
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, reqTLS bool) error {
		requireTLS = reqTLS
		return nil
	}

	err := cl.deliverByMX(context.Background(), &model.Message{MailFrom: "sender@example.org", Data: []byte("x")}, "user@example.org")
	if err != nil {
		t.Fatalf("deliverByMX: %v", err)
	}
	if !requireTLS {
		t.Fatal("expected TLS required when insecure_allow_unsigned trust model is configured")
	}
}
