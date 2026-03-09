package mailauth

import (
	"strings"
	"testing"
)

func TestEvaluateRejectsMalformedMessage(t *testing.T) {
	res := Evaluate(nil, "example.com", "sender@example.com", []byte("missing-separator"))
	if res.Action != ActionReject {
		t.Fatalf("action=%s want=%s", res.Action, ActionReject)
	}
}

func TestBuildAuthResultsHeader(t *testing.T) {
	r := Result{
		SPF:   SPFResult{Result: "pass"},
		DKIM:  DKIMResult{Result: "pass", Sigs: []DKIMSigResult{{Result: "pass", Domain: "example.com"}}},
		DMARC: DMARCResult{Result: "pass", Domain: "example.com"},
		ARC:   ARCResult{Result: "none"},
	}
	v := BuildAuthResultsHeader("mx.orinoco.local", r, "sender@example.com")
	contains := []string{
		"Authentication-Results:",
		"spf=pass smtp.mailfrom=sender@example.com",
		"dkim=pass header.d=example.com",
		"dmarc=pass header.from=example.com",
		"arc=none",
	}
	for _, c := range contains {
		if !strings.Contains(v, c) {
			t.Fatalf("header missing %q: %q", c, v)
		}
	}
}

func TestInjectHeaders(t *testing.T) {
	raw := []byte("From: a@example.com\r\n\r\nbody")
	got := string(InjectHeaders(raw, []string{"Authentication-Results: x", "X-Orinoco-Quarantine: true"}))
	if !strings.HasPrefix(got, "Authentication-Results: x\r\nX-Orinoco-Quarantine: true\r\nFrom: a@example.com") {
		t.Fatalf("unexpected injected headers: %q", got)
	}
	if !strings.HasSuffix(got, "\r\n\r\nbody") {
		t.Fatalf("body separator not preserved: %q", got)
	}
}
