package dkim2

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type staticResolver struct {
	records map[string][]string
	err     error
}

func (resolver staticResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if resolver.err != nil {
		return nil, resolver.err
	}
	return resolver.records[name], nil
}

func TestVerifyEd25519Vector(t *testing.T) {
	raw, resolver := signedEd25519Message(t)
	result := Verify(context.Background(), raw, "sender@example.test", []string{"user@recipient.test"}, DefaultLimits(), resolver)
	if result.Result != "pass" || result.Queries != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyFailureVectors(t *testing.T) {
	raw, resolver := signedEd25519Message(t)
	tamperedSignature := append([]byte(nil), raw...)
	signatureAt := strings.LastIndex(string(tamperedSignature), ":") + 1
	if tamperedSignature[signatureAt] == 'A' {
		tamperedSignature[signatureAt] = 'B'
	} else {
		tamperedSignature[signatureAt] = 'A'
	}
	badKey := staticResolver{records: map[string][]string{
		"sel._domainkey.example.test": {"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))},
	}}
	tests := []struct {
		name     string
		raw      []byte
		resolver TXTResolver
		want     string
		reason   string
	}{
		{name: "body hash mismatch", raw: []byte(strings.Replace(string(raw), "body\r\n", "changed\r\n", 1)), resolver: resolver, want: "fail", reason: "body hash mismatch"},
		{name: "header hash mismatch", raw: []byte(strings.Replace(string(raw), "Subject: example", "Subject: changed", 1)), resolver: resolver, want: "fail", reason: "header hash mismatch"},
		{name: "signature mismatch", raw: tamperedSignature, resolver: resolver, want: "fail", reason: "incorrect signature"},
		{name: "wrong public key", raw: raw, resolver: badKey, want: "fail", reason: "incorrect signature"},
		{name: "missing public key", raw: raw, resolver: staticResolver{records: map[string][]string{}}, want: "permerror", reason: "exactly one record"},
		{name: "DNS failure", raw: raw, resolver: staticResolver{err: errors.New("timeout")}, want: "temperror", reason: "could not be fetched"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Verify(context.Background(), test.raw, "sender@example.test", []string{"user@recipient.test"}, DefaultLimits(), test.resolver)
			if result.Result != test.want || !strings.Contains(result.Reason, test.reason) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestVerifyDNSQueryLimit(t *testing.T) {
	raw, resolver := signedEd25519MessageWithSelectors(t, []string{"first", "second"})
	limits := DefaultLimits()
	limits.MaxDNSQueries = 1
	result := Verify(context.Background(), raw, "sender@example.test", []string{"user@recipient.test"}, limits, resolver)
	if result.Result != "temperror" || !strings.Contains(result.Reason, "query limit") {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyDNSResponseSizeLimit(t *testing.T) {
	raw, resolver := signedEd25519Message(t)
	limits := DefaultLimits()
	limits.MaxDNSResponseBytes = 8
	result := Verify(context.Background(), raw, "sender@example.test", []string{"user@recipient.test"}, limits, resolver)
	if result.Result != "permerror" || !strings.Contains(result.Reason, "response size limit") {
		t.Fatalf("result=%+v", result)
	}
}

func TestVerifyRSASignature(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("canonical DKIM2 input\r\n")
	digest := sha256.Sum256(input)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	record := "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
	set := signatureSet{selector: "rsa", algorithm: "rsa-sha256", value: signature}
	if err := verifySignature(input, set, record); err != nil {
		t.Fatal(err)
	}
	set.value[0] ^= 1
	if err := verifySignature(input, set, record); !errors.Is(err, errIncorrectSignature) {
		t.Fatalf("error=%v", err)
	}
}

func TestParseKeyRecordRejectsMalformedAndRevokedKeys(t *testing.T) {
	for _, record := range []string{
		"v=DKIM2; k=ed25519; p=AAAA",
		"k=ed25519; p=",
		"v=DKIM1; k=ed25519; p=%%%",
		"v=DKIM1; p=AAAA; p=BBBB",
	} {
		if _, _, err := parseKeyRecord(record); err == nil {
			t.Fatalf("record %q unexpectedly accepted", record)
		}
	}
}

func signedEd25519Message(t *testing.T) ([]byte, staticResolver) {
	return signedEd25519MessageWithSelectors(t, []string{"sel"})
}

func signedEd25519MessageWithSelectors(t *testing.T, selectors []string) ([]byte, staticResolver) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("body\r\n")
	ordinary := []field{{name: "From", value: "sender@example.test"}, {name: "Subject", value: "example"}}
	headerDigest := sha256.Sum256(canonicalMessageHeaders(ordinary))
	bodyDigest := sha256.Sum256(canonicalBody(body))
	instanceValue := fmt.Sprintf("m=1; h=sha256:%s:%s", base64.StdEncoding.EncodeToString(headerDigest[:]), base64.StdEncoding.EncodeToString(bodyDigest[:]))
	mf := base64.StdEncoding.EncodeToString([]byte("<sender@example.test>"))
	rt := base64.StdEncoding.EncodeToString([]byte("<user@recipient.test>"))
	unsignedSets := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		unsignedSets = append(unsignedSets, selector+":ed25519-sha256:")
	}
	signatureValue := fmt.Sprintf("i=1; m=1; t=%d; mf=%s; rt=%s; d=example.test; s=%s", time.Now().Unix(), mf, rt, strings.Join(unsignedSets, ","))
	input := canonicalSignatureInput(
		[]field{{name: "Message-Instance", value: instanceValue}},
		nil,
		field{name: "DKIM2-Signature", value: signatureValue},
	)
	digest := sha256.Sum256(input)
	signature := ed25519.Sign(privateKey, digest[:])
	signedSets := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		signedSets = append(signedSets, selector+":ed25519-sha256:"+base64.StdEncoding.EncodeToString(signature))
	}
	signatureValue = strings.Replace(signatureValue, strings.Join(unsignedSets, ","), strings.Join(signedSets, ","), 1)
	raw := []byte(strings.Join([]string{
		"From: sender@example.test",
		"Subject: example",
		"Message-Instance: " + instanceValue,
		"DKIM2-Signature: " + signatureValue,
		"",
		"body",
		"",
	}, "\r\n"))
	records := map[string][]string{}
	for _, selector := range selectors {
		records[selector+"._domainkey.example.test"] = []string{"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(publicKey)}
	}
	resolver := staticResolver{records: records}
	return raw, resolver
}
