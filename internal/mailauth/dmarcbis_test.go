package mailauth

import (
	"context"
	"net"
	"testing"
)

func withDMARCBisDNS(t *testing.T, lookup func(context.Context, string) ([]string, error)) {
	t.Helper()
	old := dmarcBisLookupTXT
	dmarcBisLookupTXT = lookup
	t.Cleanup(func() { dmarcBisLookupTXT = old })
}

func TestDMARCBisTreeWalkExactAndPSD(t *testing.T) {
	oldExists := dmarcBisDomainExists
	dmarcBisDomainExists = func(context.Context, string) (bool, error) { return true, nil }
	t.Cleanup(func() { dmarcBisDomainExists = oldExists })
	withDMARCBisDNS(t, func(_ context.Context, name string) ([]string, error) {
		switch name {
		case "_dmarc.mail.example.test":
			return nil, nil
		case "_dmarc.example.test":
			return []string{"v=DMARC1; p=reject; psd=y"}, nil
		default:
			return nil, nil
		}
	})
	result := EvalDMARCbis(context.Background(), "mail.example.test", SPFResult{Result: "pass", Domain: "mail.example.test"}, DKIMResult{})
	if result.Result != "pass" || result.PolicyDomain != "example.test" || result.OrgDomain != "mail.example.test" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDMARCBisTreeWalkLimitsQueriesToEight(t *testing.T) {
	queries := 0
	withDMARCBisDNS(t, func(_ context.Context, _ string) ([]string, error) { queries++; return nil, nil })
	_, err := dmarcBisTreeWalk(context.Background(), "a.b.c.d.e.f.g.h.i.j.k.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if queries != 8 {
		t.Fatalf("queries=%d want 8", queries)
	}
}

func TestDMARCBisRejectsMultipleAndMalformedRecords(t *testing.T) {
	for _, value := range []string{
		"p=reject; v=DMARC1",
		"v=DMARC1; p=reject; p=none",
		"v=DMARC2; p=reject",
		"v=DMARC1; p=invalid",
		"v=DMARC1; p=reject; np=invalid",
	} {
		if _, ok := parseDMARCbisRecord(value); ok {
			t.Errorf("accepted %q", value)
		}
	}
	withDMARCBisDNS(t, func(_ context.Context, _ string) ([]string, error) {
		return []string{"v=DMARC1; p=reject", "v=DMARC1; p=none"}, nil
	})
	records, err := dmarcBisTreeWalk(context.Background(), "example.test")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v err=%v", records, err)
	}
}

func TestDMARCBisPropagatesTemporaryLookupFailure(t *testing.T) {
	withDMARCBisDNS(t, func(_ context.Context, name string) ([]string, error) {
		if name == "_dmarc.author.example" {
			return nil, &net.DNSError{Err: "timeout", Name: name, IsTimeout: true}
		}
		return nil, nil
	})
	result := EvalDMARCbis(context.Background(), "author.example", SPFResult{}, DKIMResult{})
	if result.Result != "temperror" {
		t.Fatalf("result=%+v", result)
	}
}

func TestEvaluateDMARCbisDoesNotChangeAction(t *testing.T) {
	old := dmarcLookupTXT
	dmarcLookupTXT = func(context.Context, string) ([]string, error) { return []string{"v=DMARC1; p=none"}, nil }
	t.Cleanup(func() { dmarcLookupTXT = old })
	withDMARCBisDNS(t, func(context.Context, string) ([]string, error) { return []string{"v=DMARC1; p=reject; t=n"}, nil })
	message := []byte("From: sender@example.test\r\nSubject: test\r\n\r\nbody\r\n")
	got := EvaluateWithDMARCbis(net.IPv4(192, 0, 2, 1), "mx.example", "user@example.test", message, DefaultSPFPolicy())
	if got.Action != ActionAccept {
		t.Fatalf("action=%s", got.Action)
	}
	if got.DMARC.DMARCbis == nil {
		t.Fatal("missing shadow result")
	}
}
