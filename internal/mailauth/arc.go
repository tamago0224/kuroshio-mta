package mailauth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

func EvalARC(headers []Header, body string) ARCResult {
	seals := HeaderValues(headers, "ARC-Seal")
	msgs := HeaderValues(headers, "ARC-Message-Signature")
	aars := HeaderValues(headers, "ARC-Authentication-Results")
	if len(seals) == 0 && len(msgs) == 0 && len(aars) == 0 {
		return ARCResult{Result: "none"}
	}
	if len(seals) != len(msgs) || len(seals) != len(aars) {
		return ARCResult{Result: "fail", Reason: "arc set counts mismatch"}
	}
	n := len(seals)
	for i := 1; i <= n; i++ {
		if !hasInstance(seals, i) || !hasInstance(msgs, i) || !hasInstance(aars, i) {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("missing instance i=%d", i)}
		}
		ams, ok := valueByInstance(msgs, i)
		if !ok {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("missing arc message signature i=%d", i)}
		}
		if err := verifyARCMessageSignature(headers, body, ams); err != nil {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("arc message signature verify failed i=%d: %v", i, err)}
		}
		seal, ok := valueByInstance(seals, i)
		if !ok {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("missing arc seal i=%d", i)}
		}
		if err := verifyARCSeal(headers, seal, i); err != nil {
			return ARCResult{Result: "fail", Reason: fmt.Sprintf("arc seal verify failed i=%d: %v", i, err)}
		}
	}
	if !strings.Contains(strings.ToLower(seals[n-1]), "cv=") {
		return ARCResult{Result: "fail", Reason: "missing cv in latest seal"}
	}
	return ARCResult{Result: "pass", Reason: "arc chain cryptographically valid"}
}

func hasInstance(values []string, want int) bool {
	for _, v := range values {
		tags := parseTagList(v)
		i, _ := strconv.Atoi(tags["i"])
		if i == want {
			return true
		}
	}
	return false
}

func valueByInstance(values []string, want int) (string, bool) {
	for _, v := range values {
		tags := parseTagList(v)
		i, _ := strconv.Atoi(tags["i"])
		if i == want {
			return v, true
		}
	}
	return "", false
}

func verifyARCMessageSignature(headers []Header, body, sig string) error {
	tags := parseTagList(sig)
	domain := strings.ToLower(tags["d"])
	selector := tags["s"]
	algo := strings.ToLower(tags["a"])
	if tags["i"] == "" || domain == "" || selector == "" || algo != "rsa-sha256" {
		return fmt.Errorf("unsupported signature parameters")
	}
	if strings.TrimSpace(tags["h"]) == "" || strings.TrimSpace(tags["bh"]) == "" {
		return fmt.Errorf("missing h/bh tag")
	}
	canonH, canonB := parseCanon(tags["c"])

	canonBody, err := canonicalizeBodyWithLength(body, canonB, tags["l"])
	if err != nil {
		return err
	}
	bodyHash := sha256.Sum256(canonBody)
	expectedBH, err := base64.StdEncoding.DecodeString(strings.TrimSpace(tags["bh"]))
	if err != nil || !equalBytes(bodyHash[:], expectedBH) {
		return fmt.Errorf("body hash mismatch")
	}

	signedData, err := buildSignedData(headers, tags["h"], sig, canonH, "ARC-Message-Signature")
	if err != nil {
		return err
	}
	pub, err := lookupDKIMKey(domain, selector)
	if err != nil {
		return err
	}
	sigBytes, err := base64.StdEncoding.DecodeString(compactBTag(tags["b"]))
	if err != nil {
		return fmt.Errorf("invalid b tag")
	}
	hash := sha256.Sum256([]byte(signedData))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sigBytes); err != nil {
		return fmt.Errorf("signature verify failed")
	}
	return nil
}

func verifyARCSeal(headers []Header, seal string, instance int) error {
	tags := parseTagList(seal)
	domain := strings.ToLower(tags["d"])
	selector := tags["s"]
	algo := strings.ToLower(tags["a"])
	if tags["i"] == "" || domain == "" || selector == "" || algo != "rsa-sha256" {
		return fmt.Errorf("unsupported seal parameters")
	}
	if strings.TrimSpace(tags["cv"]) == "" {
		return fmt.Errorf("missing cv tag")
	}
	chainData, err := buildARCSealSignedData(headers, instance)
	if err != nil {
		return err
	}
	pub, err := lookupDKIMKey(domain, selector)
	if err != nil {
		return err
	}
	sigBytes, err := base64.StdEncoding.DecodeString(compactBTag(tags["b"]))
	if err != nil {
		return fmt.Errorf("invalid b tag")
	}
	hash := sha256.Sum256([]byte(chainData))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sigBytes); err != nil {
		return fmt.Errorf("seal verify failed")
	}
	return nil
}

func buildARCSealSignedData(headers []Header, maxInstance int) (string, error) {
	if maxInstance <= 0 {
		return "", fmt.Errorf("invalid instance")
	}
	var out strings.Builder
	for i := 1; i <= maxInstance; i++ {
		aar, ok := headerByInstance(headers, "ARC-Authentication-Results", i)
		if !ok {
			return "", fmt.Errorf("missing arc-authentication-results i=%d", i)
		}
		ams, ok := headerByInstance(headers, "ARC-Message-Signature", i)
		if !ok {
			return "", fmt.Errorf("missing arc-message-signature i=%d", i)
		}
		as, ok := headerByInstance(headers, "ARC-Seal", i)
		if !ok {
			return "", fmt.Errorf("missing arc-seal i=%d", i)
		}
		if i == maxInstance {
			as = stripBTag(as)
		}
		out.WriteString(canonHeader("ARC-Authentication-Results", aar, "relaxed"))
		out.WriteString(canonHeader("ARC-Message-Signature", ams, "relaxed"))
		out.WriteString(canonHeader("ARC-Seal", as, "relaxed"))
	}
	return out.String(), nil
}

func headerByInstance(headers []Header, name string, want int) (string, bool) {
	for _, h := range headers {
		if !strings.EqualFold(h.Name, name) {
			continue
		}
		tags := parseTagList(h.Value)
		i, _ := strconv.Atoi(tags["i"])
		if i == want {
			return h.Value, true
		}
	}
	return "", false
}
