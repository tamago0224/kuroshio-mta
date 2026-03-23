package mailauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dkimsigner "github.com/tamago0224/kuroshio-mta/internal/dkim"
)

func TestAggregateDKIMResults(t *testing.T) {
	tests := []struct {
		name    string
		results []DKIMSigResult
		want    string
	}{
		{
			name:    "empty is none",
			results: nil,
			want:    "none",
		},
		{
			name: "any pass wins",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "pass"},
				{Result: "temperror"},
			},
			want: "pass",
		},
		{
			name: "temperror beats fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "temperror"},
			},
			want: "temperror",
		},
		{
			name: "permerror beats fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "permerror"},
			},
			want: "permerror",
		},
		{
			name: "temperror beats permerror",
			results: []DKIMSigResult{
				{Result: "permerror"},
				{Result: "temperror"},
			},
			want: "temperror",
		},
		{
			name: "all fail stays fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "fail"},
			},
			want: "fail",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateDKIMResults(tc.results); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestEvalDKIMWithoutSignatureReturnsNone(t *testing.T) {
	headers := []Header{{Name: "From", Value: "sender@example.com"}}
	got := EvalDKIM(headers, "body")
	if got.Result != "none" {
		t.Fatalf("result=%q want=none", got.Result)
	}
	if len(got.Sigs) != 0 {
		t.Fatalf("len(sigs)=%d want=0", len(got.Sigs))
	}
}

func TestVerifyDKIMSigMissingRequiredTagsArePermerror(t *testing.T) {
	tests := []struct {
		name string
		sig  string
		want string
	}{
		{
			name: "missing bh tag",
			sig:  "v=1; a=rsa-sha256; d=example.com; s=s1; h=from; b=abc",
			want: "missing bh tag",
		},
		{
			name: "missing b tag",
			sig:  "v=1; a=rsa-sha256; d=example.com; s=s1; h=from; bh=YWJj",
			want: "missing b tag",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := verifyDKIMSig([]Header{{Name: "From", Value: "sender@example.com"}}, "body", tc.sig)
			if got.Result != "permerror" {
				t.Fatalf("result=%q want=permerror reason=%q", got.Result, got.Reason)
			}
			if got.Reason != tc.want {
				t.Fatalf("reason=%q want=%q", got.Reason, tc.want)
			}
		})
	}
}

func TestCanonicalizeBodyWithLength(t *testing.T) {
	got, err := canonicalizeBodyWithLength("a\r\nb\r\n", "simple", "3")
	if err != nil {
		t.Fatalf("canonicalizeBodyWithLength: %v", err)
	}
	if string(got) != "a\r\n" {
		t.Fatalf("got=%q want=%q", string(got), "a\r\n")
	}
}

func TestCanonicalizeBodyWithLengthRejectsInvalidTag(t *testing.T) {
	if _, err := canonicalizeBodyWithLength("a\r\n", "simple", "-1"); err == nil {
		t.Fatal("expected invalid l tag error")
	}
	if _, err := canonicalizeBodyWithLength("a\r\n", "simple", "999"); err == nil {
		t.Fatal("expected oversized l tag error")
	}
}

func TestValidateDKIMTimeTags(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	if err := validateDKIMTimeTags(map[string]string{"t": "100", "x": "300"}, now); err != nil {
		t.Fatalf("validateDKIMTimeTags valid: %v", err)
	}
	if err := validateDKIMTimeTags(map[string]string{"t": "300", "x": "200"}, now); err == nil {
		t.Fatal("expected x earlier than t")
	}
	if err := validateDKIMTimeTags(map[string]string{"x": "150"}, now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired signature error, got=%v", err)
	}
	if err := validateDKIMTimeTags(map[string]string{"t": "bad"}, now); err == nil {
		t.Fatal("expected invalid t tag")
	}
}

func TestCanonicalizeBodySimplePreservesWhitespace(t *testing.T) {
	got := canonicalizeBody(" line with  spaces \r\n\r\n", "simple")
	want := " line with  spaces \r\n"
	if string(got) != want {
		t.Fatalf("got=%q want=%q", string(got), want)
	}
}

func TestCanonicalizeBodyRelaxedCollapsesWhitespace(t *testing.T) {
	got := canonicalizeBody(" line with \t spaces \t\r\nnext\tline \t\r\n\r\n", "relaxed")
	want := "line with spaces\r\nnext line\r\n"
	if string(got) != want {
		t.Fatalf("got=%q want=%q", string(got), want)
	}
}

func TestCanonicalizeBodyEmptyBecomesCRLF(t *testing.T) {
	got := canonicalizeBody("", "simple")
	if string(got) != "\r\n" {
		t.Fatalf("got=%q want=%q", string(got), "\r\n")
	}
}

func TestCanonHeaderSimplePreservesNameAndSpacing(t *testing.T) {
	got := canonHeader("Subject", "  Hello   World  ", "simple")
	want := "Subject:  Hello   World  \r\n"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestCanonHeaderRelaxedNormalizesNameAndSpacing(t *testing.T) {
	got := canonHeader("SuBject", "  Hello \t  World  ", "relaxed")
	want := "subject:Hello World\r\n"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestBuildSignedDataUsesHeadersFromBottom(t *testing.T) {
	headers := []Header{
		{Name: "Received", Value: "from a"},
		{Name: "Subject", Value: "first"},
		{Name: "Subject", Value: "second"},
		{Name: "From", Value: "sender@example.com"},
	}

	got, err := buildSignedData(headers, "subject:from", "v=1; a=rsa-sha256; b=abc123; h=subject:from", "simple", "DKIM-Signature")
	if err != nil {
		t.Fatalf("buildSignedData: %v", err)
	}
	wantContains := []string{
		"Subject:second\r\n",
		"From:sender@example.com\r\n",
		"DKIM-Signature:v=1; a=rsa-sha256; b=; h=subject:from\r\n",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("signed data missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "Subject:first\r\n") {
		t.Fatalf("signed data should use bottom-most subject, got=%q", got)
	}
}

func TestLookupDKIMKeyClassifiesDNSAndKeyErrors(t *testing.T) {
	origLookup := dkimLookupTXT
	t.Cleanup(func() {
		dkimLookupTXT = origLookup
	})

	tests := []struct {
		name    string
		txt     []string
		err     error
		wantRes string
	}{
		{
			name:    "dns error is temperror",
			err:     errors.New("dns timeout"),
			wantRes: "temperror",
		},
		{
			name:    "missing p tag is permerror",
			txt:     []string{"v=DKIM1; k=rsa;"},
			wantRes: "permerror",
		},
		{
			name:    "invalid p tag is permerror",
			txt:     []string{"v=DKIM1; k=rsa; p=!!!"},
			wantRes: "permerror",
		},
		{
			name:    "non rsa key is permerror",
			txt:     []string{"v=DKIM1; k=rsa; p=" + mustEncodeEd25519PublicKey(t)},
			wantRes: "permerror",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
				return tc.txt, tc.err
			}
			_, err := lookupDKIMKey("example.com", "s1")
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantRes == "temperror" {
				if _, ok := err.(*dkimLookupError); ok {
					t.Fatalf("dns error should remain generic, got typed error: %v", err)
				}
				return
			}
			lerr, ok := err.(*dkimLookupError)
			if !ok {
				t.Fatalf("expected dkimLookupError, got %T", err)
			}
			if lerr.Result != tc.wantRes {
				t.Fatalf("result=%q want=%q", lerr.Result, tc.wantRes)
			}
		})
	}
}

func TestLookupDKIMKeyValidRSAKey(t *testing.T) {
	origLookup := dkimLookupTXT
	t.Cleanup(func() {
		dkimLookupTXT = origLookup
	})

	pubDER := mustEncodeRSAPublicKey(t)
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		return []string{"v=DKIM1; k=rsa; p=" + pubDER}, nil
	}

	pub, err := lookupDKIMKey("example.com", "s1")
	if err != nil {
		t.Fatalf("lookupDKIMKey: %v", err)
	}
	if pub == nil {
		t.Fatal("expected rsa public key")
	}
}

func mustEncodeRSAPublicKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func mustEncodeEd25519PublicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func TestDKIMSignerAndVerifierInteropPass(t *testing.T) {
	origLookup := dkimLookupTXT
	t.Cleanup(func() {
		dkimLookupTXT = origLookup
	})

	keyPath, pubB64 := writeInteropRSAKey(t)
	signer, err := dkimsigner.NewFileSigner("example.com", "s1", keyPath, "from:to:subject")
	if err != nil {
		t.Fatalf("NewFileSigner: %v", err)
	}
	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: hi\r\n\r\nhello world")
	signed, err := signer.Sign(raw)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name == "s1._domainkey.example.com" {
			return []string{"v=DKIM1; k=rsa; p=" + pubB64}, nil
		}
		return nil, nil
	}

	headerPart, bodyPart, err := SplitMessage(signed)
	if err != nil {
		t.Fatalf("SplitMessage: %v", err)
	}
	headers, err := ParseHeaders(headerPart)
	if err != nil {
		t.Fatalf("ParseHeaders: %v", err)
	}
	got := EvalDKIM(headers, bodyPart)
	if got.Result != "pass" {
		t.Fatalf("result=%q reason=%+v", got.Result, got.Sigs)
	}
}

func TestDKIMSignerAndVerifierInteropFailOnBodyTamper(t *testing.T) {
	origLookup := dkimLookupTXT
	t.Cleanup(func() {
		dkimLookupTXT = origLookup
	})

	keyPath, pubB64 := writeInteropRSAKey(t)
	signer, err := dkimsigner.NewFileSigner("example.com", "s1", keyPath, "from:to:subject")
	if err != nil {
		t.Fatalf("NewFileSigner: %v", err)
	}
	raw := []byte("From: sender@example.com\r\nTo: rcpt@example.net\r\nSubject: hi\r\n\r\nhello world")
	signed, err := signer.Sign(raw)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	tampered := strings.Replace(string(signed), "hello world", "tampered world", 1)
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name == "s1._domainkey.example.com" {
			return []string{"v=DKIM1; k=rsa; p=" + pubB64}, nil
		}
		return nil, nil
	}

	headerPart, bodyPart, err := SplitMessage([]byte(tampered))
	if err != nil {
		t.Fatalf("SplitMessage: %v", err)
	}
	headers, err := ParseHeaders(headerPart)
	if err != nil {
		t.Fatalf("ParseHeaders: %v", err)
	}
	got := EvalDKIM(headers, bodyPart)
	if got.Result != "fail" {
		t.Fatalf("result=%q want=fail details=%+v", got.Result, got.Sigs)
	}
}

func writeInteropRSAKey(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	keyPath := filepath.Join(t.TempDir(), "dkim.pem")
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return keyPath, base64.StdEncoding.EncodeToString(pubDER)
}
