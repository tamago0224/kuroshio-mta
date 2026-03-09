//go:build integration

package integration

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/mailauth"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/router"
)

func TestMailAuthSPFAndDMARCWithDNSMock(t *testing.T) {
	spf := mailauth.EvalSPF(net.ParseIP("192.0.2.10"), "sender@example.test", "mx.example.test")
	if spf.Result != "pass" {
		t.Fatalf("spf result=%s reason=%s", spf.Result, spf.Reason)
	}

	dmarc := mailauth.EvalDMARC("example.test", spf, mailauth.DKIMResult{Result: "none"})
	if dmarc.Result != "pass" {
		t.Fatalf("dmarc result=%s reason=%s", dmarc.Result, dmarc.Reason)
	}
}

func TestOutboundDANELookupWithDNSMock(t *testing.T) {
	r := delivery.NewDANEResolver(2*time.Second, nil)
	res, err := r.LookupHost(context.Background(), "mx1.outbound.test", 25)
	if err != nil {
		t.Fatalf("dane lookup: %v", err)
	}
	if !res.HasUsableTLSA() {
		t.Fatalf("expected usable tlsa, got: %+v", res)
	}
}

func TestOutboundPolicyPrecedenceDANEOverMTASTS(t *testing.T) {
	cfg := config.Config{
		DeliveryMode:       "mx",
		DialTimeout:        2 * time.Second,
		MTASTSCacheTTL:     time.Minute,
		MTASTSFetchTimeout: time.Second,
	}
	cl := delivery.NewClient(cfg)
	cl.ResolveForTest(
		func(string, time.Duration) ([]router.MXHost, error) {
			return []router.MXHost{
				{Host: "mx1.outbound.test", Pref: 10},
			}, nil
		},
		func(_ string) (delivery.MTASTSPolicy, error) {
			return delivery.MTASTSPolicy{
				Version: "STSv1",
				Mode:    "enforce",
				MX:      []string{"blocked.outbound.test"},
				MaxAge:  time.Hour,
			}, nil
		},
	)

	called := false
	cl.DeliverHostForTest(func(_ string, _ int, _ *model.Message, _ string, requireTLS bool) error {
		called = true
		if !requireTLS {
			t.Fatal("expected TLS required when DANE is active")
		}
		return nil
	})

	err := cl.Deliver(context.Background(), &model.Message{ID: "m1", MailFrom: "sender@example.test", Data: []byte("x")}, "rcpt@outbound.test")
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !called {
		t.Fatal("deliver host should be called")
	}
}

func TestMTASTSPolicyFetchFromPolicyService(t *testing.T) {
	url := os.Getenv("INTEG_MTA_STS_POLICY_URL")
	if url == "" {
		t.Skip("INTEG_MTA_STS_POLICY_URL is not set")
	}
	resolver := delivery.NewMTASTSResolver(time.Minute, 2*time.Second, func(_ context.Context, _ string) (string, error) {
		resp, err := http.Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})
	p, err := resolver.Lookup(context.Background(), "outbound.test")
	if err != nil {
		t.Fatalf("mta-sts lookup: %v", err)
	}
	if p.Mode != "enforce" || !p.AllowsMX("mx1.outbound.test") {
		t.Fatalf("unexpected policy: %+v", p)
	}
}
