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

func TestDMARCBisTreeWalkTreatsNXDOMAINAndNODATAAsNoRecord(t *testing.T) {
	tests := []struct {
		name   string
		lookup func(context.Context, string) ([]string, error)
	}{
		{name: "NODATA", lookup: func(context.Context, string) ([]string, error) { return nil, nil }},
		{name: "NXDOMAIN", lookup: func(_ context.Context, name string) ([]string, error) {
			return nil, &net.DNSError{Name: name, Err: "no such host", IsNotFound: true}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDMARCBisDNS(t, tt.lookup)
			got := EvalDMARCbis(context.Background(), "mail.example.test", SPFResult{}, DKIMResult{})
			if got.Result != "none" {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestDMARCBisUsesNPForNonexistentAuthorDomain(t *testing.T) {
	oldExists := dmarcBisDomainExists
	dmarcBisDomainExists = func(context.Context, string) (bool, error) { return false, nil }
	t.Cleanup(func() { dmarcBisDomainExists = oldExists })
	withDMARCBisDNS(t, func(_ context.Context, name string) ([]string, error) {
		if name == "_dmarc.example.test" {
			return []string{"v=DMARC1; p=none; sp=quarantine; np=reject; psd=n"}, nil
		}
		return nil, nil
	})
	got := EvalDMARCbis(context.Background(), "missing.example.test", SPFResult{}, DKIMResult{})
	if got.Result != "fail" || got.Policy != "reject" || got.PolicyDomain != "example.test" {
		t.Fatalf("result=%+v", got)
	}
}

func TestDMARCBisStrictAndRelaxedAlignment(t *testing.T) {
	for _, tt := range []struct {
		name, mode, want string
	}{
		{name: "relaxed", mode: "r", want: "pass"},
		{name: "strict", mode: "s", want: "fail"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withDMARCBisDNS(t, func(_ context.Context, name string) ([]string, error) {
				if name == "_dmarc.example.test" {
					return []string{"v=DMARC1; p=reject; psd=n; adkim=" + tt.mode}, nil
				}
				return nil, nil
			})
			got := EvalDMARCbis(context.Background(), "example.test", SPFResult{}, DKIMResult{Sigs: []DKIMSigResult{{Result: "pass", Domain: "mail.example.test"}}})
			if got.Result != tt.want {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestDMARCBisRejectsMultipleAndMalformedRecords(t *testing.T) {
	for _, value := range []string{
		"; v=DMARC1; p=reject",
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

func TestDMARCBisTestingReducesAssessmentPolicy(t *testing.T) {
	tests := []struct {
		name       string
		record     string
		wantPolicy string
		wantTest   string
	}{
		{name: "default applies reject", record: "v=DMARC1; p=reject", wantPolicy: "reject", wantTest: "n"},
		{name: "testing reduces reject", record: "v=DMARC1; p=reject; t=y", wantPolicy: "quarantine", wantTest: "y"},
		{name: "testing reduces quarantine", record: "v=DMARC1; p=quarantine; t=y", wantPolicy: "none", wantTest: "y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDMARCBisDNS(t, func(context.Context, string) ([]string, error) { return []string{tt.record}, nil })
			got := EvalDMARCbis(context.Background(), "example.test", SPFResult{Result: "fail"}, DKIMResult{})
			if got.Result != "fail" || got.Policy != tt.wantPolicy || got.Testing != tt.wantTest {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestDMARCBisPreservesUnknownExtensionTags(t *testing.T) {
	got, ok := parseDMARCbisRecord("v=DMARC1; p=reject; x-vendor=opaque")
	if !ok || got["x-vendor"] != "opaque" {
		t.Fatalf("unknown extension was not preserved: tags=%v ok=%v", got, ok)
	}
}

func TestDMARCBisDefaultsMissingPolicyToNone(t *testing.T) {
	got, ok := parseDMARCbisRecord("v=DMARC1; rua=mailto:reports@example.test")
	if !ok || got["p"] != "none" {
		t.Fatalf("tags=%v ok=%v", got, ok)
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
