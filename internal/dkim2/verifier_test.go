package dkim2

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEvaluateStructuralVectors(t *testing.T) {
	now := time.Now().Unix()
	mf := base64.StdEncoding.EncodeToString([]byte("<sender@example.test>"))
	rt := base64.StdEncoding.EncodeToString([]byte("<user@recipient.test>"))
	valid := message(
		"Message-Instance: m=1; h=sha256:AAAA:BBBB",
		fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=sel:ed25519-sha256:AAAA", now, mf, rt),
	)
	tests := []struct {
		name       string
		raw        []byte
		mailFrom   string
		recipients []string
		want       string
		reason     string
	}{
		{name: "valid structure", raw: valid, mailFrom: "sender@example.test", recipients: []string{"user@recipient.test"}, want: "neutral", reason: "cryptographic verification not performed"},
		{name: "missing chain", raw: message(fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=sel:ed25519-sha256:AAAA", now, mf, rt)), want: "permerror", reason: "incomplete"},
		{name: "duplicate sequence", raw: message("Message-Instance: m=1; h=sha256:AAAA:BBBB", "Message-Instance: m=1; h=sha256:AAAA:BBBB", fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=sel:ed25519-sha256:AAAA", now, mf, rt)), want: "permerror", reason: "gap or duplicate"},
		{name: "expired signature", raw: message("Message-Instance: m=1; h=sha256:AAAA:BBBB", fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=sel:ed25519-sha256:AAAA", time.Now().Add(-15*24*time.Hour).Unix(), mf, rt)), want: "permerror", reason: "timestamp outside"},
		{name: "envelope mismatch", raw: valid, mailFrom: "sender@example.test", recipients: []string{"other@recipient.test"}, want: "fail", reason: "RCPT TO"},
		{name: "invalid key syntax", raw: message("Message-Instance: m=1; h=sha256:AAAA:BBBB", fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=", now, mf, rt)), want: "permerror", reason: "missing s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.raw, tt.mailFrom, tt.recipients, DefaultLimits())
			if got.Result != tt.want || !strings.Contains(got.Reason, tt.reason) {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestEvaluateRecipeAndResourceLimits(t *testing.T) {
	now := time.Now().Unix()
	mf := base64.StdEncoding.EncodeToString([]byte("<>"))
	rt := base64.StdEncoding.EncodeToString([]byte("<user@recipient.test>"))
	sig := fmt.Sprintf("DKIM2-Signature: i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=sel:ed25519-sha256:AAAA", now, mf, rt)
	tests := []struct {
		name   string
		recipe string
		mutate func(*Limits)
		want   string
		reason string
	}{
		{name: "invalid recipe", recipe: "not-base64", want: "permerror", reason: "recipe base64"},
		{name: "oversized recipe", recipe: base64.StdEncoding.EncodeToString([]byte(`{"b":[{"d":["line"]}]}`)), mutate: func(l *Limits) { l.MaxRecipeBytes = 4 }, want: "permerror", reason: "size limit"},
		{name: "deep recipe", recipe: base64.StdEncoding.EncodeToString([]byte(`{"b":[[[[[{"d":["line"]}]]]]]}`)), mutate: func(l *Limits) { l.MaxRecipeDepth = 3 }, want: "permerror", reason: "depth limit"},
		{name: "unrecoverable body is structurally valid", recipe: base64.StdEncoding.EncodeToString([]byte(`{"b":null}`)), want: "neutral"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := DefaultLimits()
			if tt.mutate != nil {
				tt.mutate(&limits)
			}
			got := Evaluate(message("Message-Instance: m=1; h=sha256:AAAA:BBBB; r="+tt.recipe, sig), "", []string{"user@recipient.test"}, limits)
			if got.Result != tt.want || !strings.Contains(got.Reason, tt.reason) {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}

func TestEvaluateChainDepthLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxChain = 1
	raw := message("Message-Instance: m=1; h=sha256:AAAA:BBBB", "Message-Instance: m=2; h=sha256:AAAA:BBBB", "DKIM2-Signature: i=1; m=1; t=1; nd=next.test; d=example.test; s=sel:ed25519-sha256:AAAA")
	got := Evaluate(raw, "", nil, limits)
	if got.Result != "permerror" || !strings.Contains(got.Reason, "depth limit") {
		t.Fatalf("result=%+v", got)
	}
}

func message(headers ...string) []byte {
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\nbody\r\n")
}
