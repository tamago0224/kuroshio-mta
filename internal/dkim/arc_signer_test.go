package dkim

import (
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(out, "\r\nARC-Authentication-Results: i=1;") {
		t.Fatalf("missing arc authentication results: %q", out)
	}
}

func TestARCFileSignerSignSkipsExistingARCSet(t *testing.T) {
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
	if string(got) != string(raw) {
		t.Fatalf("existing arc set must be untouched")
	}
}
