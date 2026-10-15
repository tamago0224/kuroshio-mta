package dkim

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
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

func TestFileSignerReloadsEd25519KeyAndAlgorithm(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "dkim.pem")
	if err := writeTestKey(keyPath); err != nil {
		t.Fatal(err)
	}
	signer, err := NewFileSigner("example.com", "s1", keyPath, "from")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: a@example.com\r\n\r\nhello\r\n")
	first, err := signer.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "a=rsa-sha256;") {
		t.Fatal("missing RSA algorithm")
	}
	for range 2 {
		previous, err := os.Stat(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		nextTime := previous.ModTime().Add(time.Second)
		if err := os.Chtimes(keyPath, nextTime, nextTime); err != nil {
			t.Fatal(err)
		}
		got, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "a=ed25519-sha256;") {
			t.Fatal("algorithm did not change with key")
		}
		if string(got) == string(first) {
			t.Fatal("signature did not change with key")
		}
		first = got
	}
	arc, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from")
	if err != nil {
		t.Fatal(err)
	}
	got, err := arc.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "ARC-Seal: i=1; cv=none; a=ed25519-sha256;") {
		t.Fatal("ARC did not use shared Ed25519 key")
	}
}

func TestFileSignerRejectsUnsupportedPrivateKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "unsupported.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileSigner("example.com", "s1", keyPath, "from"); err == nil {
		t.Fatal("ECDSA key must be rejected")
	}
	if _, err := NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from"); err == nil {
		t.Fatal("ECDSA ARC key must be rejected")
	}
}
