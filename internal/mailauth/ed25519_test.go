package mailauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"path/filepath"
	"strings"

	dkimsigner "github.com/tamago0224/kuroshio-mta/internal/dkim"
	"os"
	"testing"
)

// RFC 8463 Appendix A.3, retaining the independent Ed25519 signature only.
func TestDKIMEd25519RFC8463Vector(t *testing.T) {
	raw, err := os.ReadFile("testdata/rfc8463.eml")
	if err != nil {
		t.Fatal(err)
	}
	original := dkimLookupTXT
	t.Cleanup(func() { dkimLookupTXT = original })
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name != "brisbane._domainkey.football.example.com" {
			t.Fatalf("unexpected lookup: %s", name)
		}
		return []string{"v=DKIM1; k=ed25519; p=11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="}, nil
	}
	h, b, err := SplitMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	headers, err := ParseHeaders(h)
	if err != nil {
		t.Fatal(err)
	}
	got := EvalDKIM(headers, b)
	if got.Result != "pass" {
		t.Fatalf("RFC vector: %+v", got)
	}
}

func writeInteropEd25519Key(t *testing.T) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ed25519.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, base64.StdEncoding.EncodeToString(pub)
}

func TestDKIMEd25519SigningAndVerification(t *testing.T) {
	keyPath, publicKey := writeInteropEd25519Key(t)
	signer, err := dkimsigner.NewFileSigner("example.com", "ed", keyPath, "from:to:subject")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.net\r\nSubject: hello\r\n\r\nhello world\r\n")
	signed, err := signer.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(signed), "DKIM-Signature: v=1; a=ed25519-sha256;") {
		t.Fatalf("wrong algorithm: %s", signed)
	}
	original := dkimLookupTXT
	t.Cleanup(func() { dkimLookupTXT = original })
	tests := []struct{ name, message, record, want string }{
		{name: "valid", message: string(signed), record: "k=ed25519; p=" + publicKey, want: "pass"},
		{name: "body tampering",
			message: strings.Replace(string(signed), "hello world", "changed body", 1),
			record:  "k=ed25519; p=" + publicKey,
			want:    "fail"},
		{name: "header tampering",
			message: strings.Replace(string(signed), "Subject: hello", "Subject: changed", 1),
			record:  "k=ed25519; p=" + publicKey,
			want:    "fail"},
		{name: "invalid signature",
			message: strings.Replace(string(signed), "; b=", "; b=A", 1),
			record:  "k=ed25519; p=" + publicKey,
			want:    "permerror"},
		{name: "short key", message: string(signed), record: "k=ed25519; p=YWJj", want: "permerror"},
		{name: "long key",
			message: string(signed),
			record:  "k=ed25519; p=" + base64.StdEncoding.EncodeToString(make([]byte, 64)),
			want:    "permerror"},
		{name: "ASN1 key forbidden",
			message: string(signed),
			record:  "k=ed25519; p=" + mustEncodeEd25519PublicKey(t),
			want:    "permerror"},
		{name: "wrong key type", message: string(signed), record: "k=rsa; p=" + mustEncodeRSAPublicKey(t), want: "permerror"},
		{name: "unknown key type", message: string(signed), record: "k=unknown; p=" + publicKey, want: "permerror"},
		{name: "revoked key", message: string(signed), record: "k=ed25519; p=", want: "permerror"},
		{name: "unsupported hash",
			message: strings.Replace(string(signed), "ed25519-sha256", "ed25519-sha512", 1),
			record:  "k=ed25519; p=" + publicKey,
			want:    "permerror"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dkimLookupTXT = func(_ context.Context, _ string) ([]string, error) { return []string{tt.record}, nil }
			h, b, err := SplitMessage([]byte(tt.message))
			if err != nil {
				t.Fatal(err)
			}
			headers, err := ParseHeaders(h)
			if err != nil {
				t.Fatal(err)
			}
			got := EvalDKIM(headers, b)
			if got.Result != tt.want {
				t.Fatalf("want %s, got %+v", tt.want, got)
			}
		})
	}
}

func TestDKIMMixedRSAAndEd25519Signatures(t *testing.T) {
	rsaPath, rsaPub := writeInteropRSAKey(t)
	edPath, edPub := writeInteropEd25519Key(t)
	original := dkimLookupTXT
	t.Cleanup(func() { dkimLookupTXT = original })
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name == "rsa._domainkey.example.com" {
			return []string{"k=rsa; p=" + rsaPub}, nil
		}
		return []string{"k=ed25519; p=" + edPub}, nil
	}
	raw := []byte("From: a@example.com\r\nSubject: mixed\r\n\r\nhello\r\n")
	for _, key := range []struct{ path, selector string }{{path: rsaPath, selector: "rsa"}, {path: edPath, selector: "ed"}} {
		signer, err := dkimsigner.NewFileSigner("example.com", key.selector, key.path, "from:subject")
		if err != nil {
			t.Fatal(err)
		}
		raw, err = signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	h, b, err := SplitMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	headers, err := ParseHeaders(h)
	if err != nil {
		t.Fatal(err)
	}
	got := EvalDKIM(headers, b)
	if got.Result != "pass" || len(got.Sigs) != 2 {
		t.Fatalf("mixed signatures: %+v", got)
	}
	for _, sig := range got.Sigs {
		if sig.Result != "pass" {
			t.Fatalf("signature: %+v", sig)
		}
	}
}

func TestARCMixedRSAAndEd25519Chain(t *testing.T) {
	rsaPath, rsaPub := writeInteropRSAKey(t)
	edPath, edPub := writeInteropEd25519Key(t)
	original := dkimLookupTXT
	t.Cleanup(func() { dkimLookupTXT = original })
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name == "rsa._domainkey.example.com" {
			return []string{"k=rsa; p=" + rsaPub}, nil
		}
		return []string{"k=ed25519; p=" + edPub}, nil
	}
	raw := []byte("From: a@example.com\r\nSubject: forwarded\r\n\r\nhello\r\n")
	keys := []struct{ path, selector string }{
		{path: rsaPath, selector: "rsa"},
		{path: edPath, selector: "ed"},
		{path: rsaPath, selector: "rsa"},
	}
	for _, key := range keys {
		h, b, err := SplitMessage(raw)
		if err != nil {
			t.Fatal(err)
		}
		headers, err := ParseHeaders(h)
		if err != nil {
			t.Fatal(err)
		}
		status := EvalARC(headers, b)
		signer, err := dkimsigner.NewARCFileSigner("example.com", key.selector, key.path, "mx.example.com", "from:subject")
		if err != nil {
			t.Fatal(err)
		}
		raw, err = signer.SignWithChainValidation(raw, status.Result)
		if err != nil {
			t.Fatal(err)
		}
		h, b, err = SplitMessage(raw)
		if err != nil {
			t.Fatal(err)
		}
		headers, err = ParseHeaders(h)
		if err != nil {
			t.Fatal(err)
		}
		if got := EvalARC(headers, b); got.Result != "pass" {
			t.Fatalf("mixed ARC chain: %+v", got)
		}
	}
}
