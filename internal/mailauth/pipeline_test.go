package mailauth

import (
	"context"
	"net"
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

func TestEvaluateWithPolicy_SeparateHeloAndMailFromModes(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "bad-helo.example.net":
			return []string{"v=spf1 -all"}, nil
		case "example.com":
			return []string{"v=spf1 ip4:192.0.2.10 -all"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: x\r\n\r\nbody")

	heloEnforce := EvaluateWithPolicy(net.ParseIP("192.0.2.10"), "bad-helo.example.net", "sender@example.com", raw, SPFPolicy{
		HeloMode:     "enforce",
		MailFromMode: "advisory",
	})
	if heloEnforce.Action != ActionReject || heloEnforce.Reason != "spf helo policy" {
		t.Fatalf("helo enforce should reject, action=%s reason=%q", heloEnforce.Action, heloEnforce.Reason)
	}
	if heloEnforce.SPFHelo.Result != "fail" || heloEnforce.SPFMailFrom.Result != "pass" {
		t.Fatalf("unexpected spf results: helo=%+v mailfrom=%+v", heloEnforce.SPFHelo, heloEnforce.SPFMailFrom)
	}

	heloAdvisory := EvaluateWithPolicy(net.ParseIP("192.0.2.10"), "bad-helo.example.net", "sender@example.com", raw, SPFPolicy{
		HeloMode:     "advisory",
		MailFromMode: "advisory",
	})
	if heloAdvisory.Action == ActionReject && heloAdvisory.Reason == "spf helo policy" {
		t.Fatalf("helo advisory should not enforce reject")
	}
}

func TestEvaluateWithPolicy_MailFromEnforce(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "ok-helo.example.net":
			return []string{"v=spf1 ip4:198.51.100.20 -all"}, nil
		case "example.com":
			return []string{"v=spf1 -all"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: x\r\n\r\nbody")
	res := EvaluateWithPolicy(net.ParseIP("198.51.100.20"), "ok-helo.example.net", "sender@example.com", raw, SPFPolicy{
		HeloMode:     "off",
		MailFromMode: "enforce",
	})
	if res.Action != ActionReject || res.Reason != "spf mailfrom policy" {
		t.Fatalf("mailfrom enforce should reject, action=%s reason=%q", res.Action, res.Reason)
	}
}

func TestEvaluateWithPolicy_DMARCRejectPctZeroSampledOut(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})
	dmarcLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if strings.EqualFold(name, "_dmarc.example.com") {
			return []string{"v=DMARC1; p=reject; pct=0"}, nil
		}
		return nil, nil
	}

	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: x\r\n\r\nbody")
	res := EvaluateWithPolicy(nil, "mx.sender.test", "sender@example.com", raw, DefaultSPFPolicy())
	if res.DMARC.Result != "fail" {
		t.Fatalf("dmarc result=%s want=fail", res.DMARC.Result)
	}
	if res.Action != ActionAccept {
		t.Fatalf("action=%s want=%s", res.Action, ActionAccept)
	}
	if !strings.Contains(res.Reason, "sampled out") {
		t.Fatalf("reason=%q want contains sampled out", res.Reason)
	}
}

func TestEvaluateWithPolicy_DMARCRejectPctHundredEnforced(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})
	dmarcLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if strings.EqualFold(name, "_dmarc.example.com") {
			return []string{"v=DMARC1; p=reject; pct=100"}, nil
		}
		return nil, nil
	}

	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: x\r\n\r\nbody")
	res := EvaluateWithPolicy(nil, "mx.sender.test", "sender@example.com", raw, DefaultSPFPolicy())
	if res.DMARC.Result != "fail" {
		t.Fatalf("dmarc result=%s want=fail", res.DMARC.Result)
	}
	if res.Action != ActionReject {
		t.Fatalf("action=%s want=%s", res.Action, ActionReject)
	}
	if res.Reason != "dmarc reject policy" {
		t.Fatalf("reason=%q want=%q", res.Reason, "dmarc reject policy")
	}
}

func TestDMARCSamplingBucketDeterministic(t *testing.T) {
	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\n\r\nbody")
	a := dmarcSamplingBucket("mx.example.net", "sender@example.com", raw)
	b := dmarcSamplingBucket("mx.example.net", "sender@example.com", raw)
	if a != b {
		t.Fatalf("bucket must be deterministic: %d != %d", a, b)
	}
	if a < 0 || a > 99 {
		t.Fatalf("bucket out of range: %d", a)
	}
}

func TestEvaluateWithPolicy_ARCFailurePolicyReject(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})
	dmarcLookupTXT = func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	raw := []byte("From: sender@example.com\r\nARC-Seal: i=1; cv=none\r\n\r\nbody")
	res := EvaluateWithPolicy(nil, "mx.example.net", "sender@example.com", raw, SPFPolicy{
		HeloMode:       "off",
		MailFromMode:   "off",
		ARCFailureMode: "reject",
	})
	if res.ARC.Result != "fail" {
		t.Fatalf("arc result=%s want=fail", res.ARC.Result)
	}
	if res.Action != ActionReject {
		t.Fatalf("action=%s want=%s", res.Action, ActionReject)
	}
	if res.Reason != "arc failure policy" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestEvaluateWithPolicy_ARCFailurePolicyQuarantine(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})
	dmarcLookupTXT = func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	raw := []byte("From: sender@example.com\r\nARC-Seal: i=1; cv=none\r\n\r\nbody")
	res := EvaluateWithPolicy(nil, "mx.example.net", "sender@example.com", raw, SPFPolicy{
		HeloMode:       "off",
		MailFromMode:   "off",
		ARCFailureMode: "quarantine",
	})
	if res.ARC.Result != "fail" {
		t.Fatalf("arc result=%s want=fail", res.ARC.Result)
	}
	if res.Action != ActionQuarantine {
		t.Fatalf("action=%s want=%s", res.Action, ActionQuarantine)
	}
	if res.Reason != "arc failure policy" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestEvaluateWithPolicy_ARCFailurePolicy(t *testing.T) {
	origDMARC := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origDMARC
	})
	dmarcLookupTXT = func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}

	raw := []byte(strings.Join([]string{
		"From: sender@example.com",
		"To: rcpt@example.net",
		"Subject: x",
		"ARC-Authentication-Results: i=1; mx=example.net; dmarc=pass",
		"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=s1; h=from; bh=abc; b=abc",
		"ARC-Seal: i=1; cv=none; a=rsa-sha256; d=example.com; s=s1; b=abc",
		"",
		"body",
	}, "\r\n"))

	tests := []struct {
		mode       string
		wantAction Action
	}{
		{mode: "accept", wantAction: ActionAccept},
		{mode: "quarantine", wantAction: ActionQuarantine},
		{mode: "reject", wantAction: ActionReject},
	}
	for _, tt := range tests {
		res := EvaluateWithPolicy(nil, "mx.sender.test", "sender@example.com", raw, SPFPolicy{
			HeloMode:       "advisory",
			MailFromMode:   "advisory",
			ARCFailureMode: tt.mode,
		})
		if res.ARC.Result != "fail" {
			t.Fatalf("mode=%s arc result=%s want=fail", tt.mode, res.ARC.Result)
		}
		if res.Action != tt.wantAction {
			t.Fatalf("mode=%s action=%s want=%s", tt.mode, res.Action, tt.wantAction)
		}
		if tt.wantAction != ActionAccept && res.Reason != "arc failure policy" {
			t.Fatalf("mode=%s reason=%q", tt.mode, res.Reason)
		}
	}
}
