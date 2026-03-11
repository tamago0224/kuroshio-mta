package dkim

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileSignerSignInjectsDKIMHeader(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "dkim.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key: %v", err)
	}
	s, err := NewFileSigner("example.com", "s1", keyPath, "from:to:subject")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.net\r\nSubject: hi\r\n\r\nhello")
	got, err := s.Sign(raw)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out := string(got)
	if !strings.HasPrefix(out, "DKIM-Signature: ") {
		t.Fatalf("signed message must start with DKIM-Signature header: %q", out)
	}
	if !strings.Contains(out, " d=example.com;") || !strings.Contains(out, " s=s1;") {
		t.Fatalf("missing d/s tag: %q", out)
	}
	if !strings.Contains(out, "\r\nFrom: a@example.com\r\n") {
		t.Fatalf("original headers missing: %q", out)
	}
}

func TestFileSignerReloadsRotatedKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "dkim.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatalf("write key1: %v", err)
	}
	s, err := NewFileSigner("example.com", "s1", keyPath, "from")
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	msg := []byte("From: a@example.com\r\n\r\nhello")
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
		t.Fatal("signature should change after key rotation")
	}
}

func writeTestKey(path string) error {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return err
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}
