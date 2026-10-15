package mailauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	dkimsigner "github.com/tamago0224/kuroshio-mta/internal/dkim"
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

func TestEvalARCMultiHopCryptoPass(t *testing.T) {
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

	ams1Base := "i=1; a=rsa-sha256; d=example.com; s=s1; c=simple/simple; h=from:to:subject; bh=" + bh + "; b="
	seal1Base := "i=1; cv=none; a=rsa-sha256; d=example.com; s=s1; b="
	ams2Base := "i=2; a=rsa-sha256; d=example.com; s=s1; c=simple/simple; h=from:to:subject; bh=" + bh + "; b="
	seal2Base := "i=2; cv=pass; a=rsa-sha256; d=example.com; s=s1; b="
	headers := []Header{
		{Name: "From", Value: "a@example.com"},
		{Name: "To", Value: "b@example.net"},
		{Name: "Subject", Value: "arc multi hop"},
		{Name: "ARC-Authentication-Results", Value: "i=1; mx=mx1.example.net; dmarc=pass"},
		{Name: "ARC-Message-Signature", Value: ams1Base},
		{Name: "ARC-Seal", Value: seal1Base},
		{Name: "ARC-Authentication-Results", Value: "i=2; mx=mx2.example.net; dmarc=pass"},
		{Name: "ARC-Message-Signature", Value: ams2Base},
		{Name: "ARC-Seal", Value: seal2Base},
	}

	ams1Data, err := buildSignedData(headers, "from:to:subject", ams1Base, "simple", "ARC-Message-Signature")
	if err != nil {
		t.Fatalf("build ams1 data: %v", err)
	}
	ams1Hash := sha256.Sum256([]byte(ams1Data))
	ams1Sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, ams1Hash[:])
	if err != nil {
		t.Fatalf("sign ams1: %v", err)
	}
	headers[4].Value = ams1Base + base64.StdEncoding.EncodeToString(ams1Sig)

	seal1Data, err := buildARCSealSignedData(headers, 1)
	if err != nil {
		t.Fatalf("build seal1 data: %v", err)
	}
	seal1Hash := sha256.Sum256([]byte(seal1Data))
	seal1Sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, seal1Hash[:])
	if err != nil {
		t.Fatalf("sign seal1: %v", err)
	}
	headers[5].Value = seal1Base + base64.StdEncoding.EncodeToString(seal1Sig)

	ams2Data, err := buildSignedData(headers, "from:to:subject", ams2Base, "simple", "ARC-Message-Signature")
	if err != nil {
		t.Fatalf("build ams2 data: %v", err)
	}
	ams2Hash := sha256.Sum256([]byte(ams2Data))
	ams2Sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, ams2Hash[:])
	if err != nil {
		t.Fatalf("sign ams2: %v", err)
	}
	headers[7].Value = ams2Base + base64.StdEncoding.EncodeToString(ams2Sig)

	seal2Data, err := buildARCSealSignedData(headers, 2)
	if err != nil {
		t.Fatalf("build seal2 data: %v", err)
	}
	seal2Hash := sha256.Sum256([]byte(seal2Data))
	seal2Sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, seal2Hash[:])
	if err != nil {
		t.Fatalf("sign seal2: %v", err)
	}
	headers[8].Value = seal2Base + base64.StdEncoding.EncodeToString(seal2Sig)

	res := EvalARC(headers, body)
	if res.Result != "pass" {
		t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
	}
}

func TestARCSignerChainRoundTrip(t *testing.T) {
	keyPath, publicKey := writeInteropRSAKey(t)
	oldLookup := dkimLookupTXT
	t.Cleanup(func() { dkimLookupTXT = oldLookup })
	dkimLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name != "s1._domainkey.example.com" {
			return nil, fmt.Errorf("unexpected lookup %s", name)
		}
		return []string{"v=DKIM1; k=rsa; p=" + publicKey}, nil
	}
	signer, err := dkimsigner.NewARCFileSigner("example.com", "s1", keyPath, "mx.example.com", "from:to:subject:authentication-results:arc-seal")
	if err != nil {
		t.Fatal(err)
	}
	parse := func(raw []byte) ([]Header, string) {
		t.Helper()
		h, b, err := SplitMessage(raw)
		if err != nil {
			t.Fatal(err)
		}
		headers, err := ParseHeaders(h)
		if err != nil {
			t.Fatal(err)
		}
		return headers, b
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.net\r\nSubject: original\r\nAuthentication-Results: mx.example.com; dmarc=pass; arc=pass\r\n\r\nhello\r\n")
	for hop := 1; hop <= 3; hop++ {
		headers, body := parse(raw)
		result := EvalARC(headers, body)
		if hop == 1 && result.Result != "none" {
			t.Fatal(result)
		}
		if hop > 1 && result.Result != "pass" {
			t.Fatal(result)
		}
		// A participating forwarder can change content after validating the input.
		if hop == 2 {
			raw = []byte(strings.ReplaceAll(string(raw), "Subject: original", "Subject: forwarded"))
		}
		previous := string(raw)
		raw, err = signer.SignWithChainValidation(raw, result.Result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(raw), previous) {
			t.Fatal("previous message modified")
		}
		headers, body = parse(raw)
		got := EvalARC(headers, body)
		if got.Result != "pass" {
			t.Fatalf("hop %d: %+v", hop, got)
		}
		ams, _ := headerByInstance(headers, "ARC-Message-Signature", hop)
		h := parseTagList(ams)["h"]
		if strings.Contains(h, "arc-") || strings.Contains(h, "authentication-results") {
			t.Fatalf("prohibited AMS header: %s", h)
		}
	}
	t.Run("earlier seal tampering", func(t *testing.T) {
		damaged := []byte(strings.Replace(string(raw), "i=1; mx.example.com", "i=1; forged.example.com", 1))
		h, b := parse(damaged)
		if result := EvalARC(h, b); result.Result != "fail" {
			t.Fatalf("tampering passed: %+v", result)
		}
	})
	t.Run("body tampering marks failure once", func(t *testing.T) {
		damaged := []byte(strings.Replace(string(raw), "hello", "tampered", 1))
		h, b := parse(damaged)
		result := EvalARC(h, b)
		if result.Result != "fail" {
			t.Fatal(result)
		}
		failed, err := signer.SignWithChainValidation(damaged, result.Result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(failed), "ARC-Seal: i=4; cv=fail;") {
			t.Fatalf("missing failure seal: %s", failed)
		}
		h, b = parse(failed)
		if got := EvalARC(h, b); got.Result != "fail" {
			t.Fatalf("failed chain recovered: %+v", got)
		}
		seal, _ := headerByInstance(h, "ARC-Seal", 4)
		if err := verifyARCSeal(h, seal, 4); err != nil {
			t.Fatalf("failure seal invalid: %v", err)
		}
		again, err := signer.SignWithChainValidation(failed, "pass")
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(failed) {
			t.Fatal("cv=fail must stop further sealing")
		}
	})
}
