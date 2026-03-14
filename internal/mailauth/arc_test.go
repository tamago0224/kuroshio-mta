package mailauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

func TestEvalARC(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		res := EvalARC([]Header{{Name: "From", Value: "a@example.com"}}, "")
		if res.Result != "none" {
			t.Fatalf("result=%s", res.Result)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Seal", Value: "i=1; cv=none"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com"},
		}
		res := EvalARC(h, "")
		if res.Result != "fail" {
			t.Fatalf("result=%s", res.Result)
		}
	})
	t.Run("duplicate_instance_fails", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Authentication-Results", Value: "i=1; mx=example"},
			{Name: "ARC-Authentication-Results", Value: "i=1; mx=example-dup"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com; a=rsa-sha256; s=s1; h=from; bh=x; b=y"},
			{Name: "ARC-Seal", Value: "i=1; cv=none; d=example.com; a=rsa-sha256; s=s1; b=z"},
		}
		res := EvalARC(h, "")
		if res.Result != "fail" {
			t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
		}
	})
	t.Run("missing_sequence_fails", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Authentication-Results", Value: "i=1; mx=example"},
			{Name: "ARC-Authentication-Results", Value: "i=3; mx=example"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com; a=rsa-sha256; s=s1; h=from; bh=x; b=y"},
			{Name: "ARC-Message-Signature", Value: "i=3; d=example.com; a=rsa-sha256; s=s1; h=from; bh=x; b=y"},
			{Name: "ARC-Seal", Value: "i=1; cv=none; d=example.com; a=rsa-sha256; s=s1; b=z"},
			{Name: "ARC-Seal", Value: "i=3; cv=pass; d=example.com; a=rsa-sha256; s=s1; b=z"},
		}
		res := EvalARC(h, "")
		if res.Result != "fail" {
			t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
		}
	})
	t.Run("fail_without_crypto", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Authentication-Results", Value: "i=1; mx=example"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com; a=rsa-sha256; s=s1; h=from; bh=x; b=y"},
			{Name: "ARC-Seal", Value: "i=1; cv=none; d=example.com; a=rsa-sha256; s=s1; b=z"},
		}
		res := EvalARC(h, "")
		if res.Result != "fail" {
			t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
		}
	})
}

func TestVerifyARCSealCVRules(t *testing.T) {
	if !validSealCV("none", 1) {
		t.Fatal("i=1 must allow cv=none")
	}
	if validSealCV("pass", 1) {
		t.Fatal("i=1 must not allow cv=pass")
	}
	if !validSealCV("pass", 2) || !validSealCV("fail", 2) {
		t.Fatal("i>1 must allow cv=pass/fail")
	}
	if validSealCV("none", 2) {
		t.Fatal("i>1 must not allow cv=none")
	}
}

func TestEvalARCCryptoPass(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	origLookup := dkimLookupTXT
	t.Cleanup(func() {
		dkimLookupTXT = origLookup
	})
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name == "s1._domainkey.example.com" {
			return []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pubDER)}, nil
		}
		return nil, nil
	}

	body := "hello\r\n"
	bodyHash := sha256.Sum256(canonicalizeBody(body, "simple"))
	bh := base64.StdEncoding.EncodeToString(bodyHash[:])
	amsBase := "i=1; a=rsa-sha256; d=example.com; s=s1; c=simple/simple; h=from:to:subject; bh=" + bh + "; b="
	headers := []Header{
		{Name: "From", Value: "a@example.com"},
		{Name: "To", Value: "b@example.net"},
		{Name: "Subject", Value: "arc test"},
		{Name: "ARC-Authentication-Results", Value: "i=1; mx=example.net; dmarc=pass"},
		{Name: "ARC-Message-Signature", Value: amsBase},
		{Name: "ARC-Seal", Value: "i=1; cv=none; a=rsa-sha256; d=example.com; s=s1; b="},
	}

	amsData, err := buildSignedData(headers, "from:to:subject", amsBase, "simple", "ARC-Message-Signature")
	if err != nil {
		t.Fatalf("build ams data: %v", err)
	}
	amsHash := sha256.Sum256([]byte(amsData))
	amsSig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, amsHash[:])
	if err != nil {
		t.Fatalf("sign ams: %v", err)
	}
	headers[4].Value = amsBase + base64.StdEncoding.EncodeToString(amsSig)

	sealData, err := buildARCSealSignedData(headers, 1)
	if err != nil {
		t.Fatalf("build seal data: %v", err)
	}
	sealHash := sha256.Sum256([]byte(sealData))
	sealSig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sealHash[:])
	if err != nil {
		t.Fatalf("sign seal: %v", err)
	}
	headers[5].Value = headers[5].Value + base64.StdEncoding.EncodeToString(sealSig)

	res := EvalARC(headers, body)
	if res.Result != "pass" {
		t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
	}
}
