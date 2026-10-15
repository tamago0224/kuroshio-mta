package dkim

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestARCFileSignerSignInjectsARCSet(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "arc.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key: %v", err)
	}
	s, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from:to:subject")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.net\r\nSubject: hi\r\nAuthentication-Results: mx.example.com; dmarc=pass\r\n\r\nhello")
	got, err := s.Sign(raw)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out := string(got)
	if !strings.HasPrefix(out, "ARC-Seal: ") {
		t.Fatalf("missing arc-seal: %q", out)
	}
	if !strings.Contains(out, "\r\nARC-Message-Signature: i=1;") {
		t.Fatalf("missing arc message signature: %q", out)
	}
	if !strings.Contains(out, "\r\nARC-Authentication-Results: i=1; mx.example.com; dmarc=pass; arc=none\r\n") {
		t.Fatalf("missing local authentication results and current ARC status: %q", out)
	}
}

func TestARCFileSignerSignMarksIncompleteChainFailed(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "arc.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key: %v", err)
	}
	s, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	raw := []byte("ARC-Seal: i=1; cv=none; a=rsa-sha256; d=example.com; s=s1; b=abc\r\nFrom: a@example.com\r\n\r\nhello")
	got, err := s.Sign(raw)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.HasPrefix(string(got), "ARC-Seal: i=2; cv=fail;") {
		t.Fatalf("incomplete chain must be marked failed: %s", got)
	}
	if !strings.HasSuffix(string(got), string(raw)) {
		t.Fatal("existing headers and body must be preserved")
	}
}

func TestARCFileSignerReloadsRotatedKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "arc.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key1: %v", err)
	}
	s, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from:to:subject")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	msg := []byte("From: a@example.com\r\nTo: b@example.net\r\nSubject: hi\r\n\r\nhello")
	first, err := s.Sign(msg)
	if err != nil {
		t.Fatalf("sign first: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key2: %v", err)
	}
	second, err := s.Sign(msg)
	if err != nil {
		t.Fatalf("sign second: %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("arc signature should change after key rotation")
	}
}

func TestARCFileSignerMalformedChains(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "arc.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatal(err)
	}
	signer, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from")
	if err != nil {
		t.Fatal(err)
	}
	set := func(instance, cv string) string {
		return "ARC-Seal: i=" + instance + "; cv=" + cv + "; b=invalid\r\n" +
			"ARC-Message-Signature: i=" + instance + "; b=invalid\r\n" +
			"ARC-Authentication-Results: i=" + instance + "; remote.example; arc=pass\r\n"
	}
	tests := []struct {
		name, chain, prefix string
		unchanged           bool
	}{
		{name: "duplicate", chain: set("1", "none") + set("1", "none"), prefix: "ARC-Seal: i=2; cv=fail;"},
		{name: "missing instance", chain: set("1", "none") + set("3", "pass"), prefix: "ARC-Seal: i=4; cv=fail;"},
		{name: "incomplete", chain: "ARC-Authentication-Results: i=2; remote.example; arc=pass\r\n", prefix: "ARC-Seal: i=3; cv=fail;"},
		{name: "invalid cv", chain: set("1", "pass"), prefix: "ARC-Seal: i=2; cv=fail;"},
		{name: "already failed", chain: set("1", "none") + set("2", "fail"), unchanged: true},
		{name: "invalid instance", chain: set("x", "none"), unchanged: true},
		{name: "zero instance", chain: set("0", "none"), unchanged: true},
		{name: "chain limit", chain: set("50", "pass"), unchanged: true},
		{name: "instance overflow", chain: set("9999999999999999999999", "pass"), unchanged: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(tt.chain + "From: a@example.com\r\n\r\nhello")
			got, err := signer.SignWithChainValidation(raw, "pass")
			if err != nil {
				t.Fatal(err)
			}
			if tt.unchanged {
				if string(got) != string(raw) {
					t.Fatal("terminal or ambiguous chain modified")
				}
				return
			}
			if !strings.HasPrefix(string(got), tt.prefix) {
				t.Fatalf("want prefix %s, got %s", tt.prefix, got)
			}
			if !strings.HasSuffix(string(got), string(raw)) {
				t.Fatal("existing message modified")
			}
		})
	}
}
