package mailauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
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

	got, err := buildSignedData(headers, "subject:from", "v=1; a=rsa-sha256; b=abc123; h=subject:from", "simple")
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
