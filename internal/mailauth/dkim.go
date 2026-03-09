package mailauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
)

func EvalDKIM(headers []Header, body string) DKIMResult {
	dkims := HeaderValues(headers, "DKIM-Signature")
	if len(dkims) == 0 {
		return DKIMResult{Result: "none"}
	}
	results := make([]DKIMSigResult, 0, len(dkims))
	pass := false
	for _, sig := range dkims {
		r := verifyDKIMSig(headers, body, sig)
		if r.Result == "pass" {
			pass = true
		}
		results = append(results, r)
	}
	out := "fail"
	if pass {
		out = "pass"
	}
	return DKIMResult{Result: out, Sigs: results}
}

func verifyDKIMSig(headers []Header, body, sig string) DKIMSigResult {
	tags := parseTagList(sig)
	domain := strings.ToLower(tags["d"])
	selector := tags["s"]
	algo := strings.ToLower(tags["a"])
	if tags["v"] != "1" || domain == "" || selector == "" || algo != "rsa-sha256" {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "unsupported signature parameters"}
	}
	canonH, canonB := parseCanon(tags["c"])

	canonBody := canonicalizeBody(body, canonB)
	h := sha256.Sum256(canonBody)
	bh := strings.TrimSpace(tags["bh"])
	expectedBH, err := base64.StdEncoding.DecodeString(bh)
	if err != nil || !equalBytes(h[:], expectedBH) {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "fail", Reason: "body hash mismatch"}
	}

	signedData, err := buildSignedData(headers, tags["h"], sig, canonH)
	if err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: err.Error()}
	}
	pub, err := lookupDKIMKey(domain, selector)
	if err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "temperror", Reason: err.Error()}
	}
	sigBytes, err := base64.StdEncoding.DecodeString(compactBTag(tags["b"]))
	if err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "invalid b tag"}
	}
	hash := sha256.Sum256([]byte(signedData))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sigBytes); err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "fail", Reason: "signature verify failed"}
	}
	return DKIMSigResult{Domain: domain, Selector: selector, Result: "pass"}
}

func parseTagList(v string) map[string]string {
	m := map[string]string{}
	parts := strings.Split(v, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.IndexByte(p, '=')
		if idx <= 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(p[:idx]))
		val := strings.TrimSpace(p[idx+1:])
		m[k] = val
	}
	return m
}

func parseCanon(c string) (string, string) {
	if c == "" {
		return "simple", "simple"
	}
	parts := strings.SplitN(strings.ToLower(c), "/", 2)
	if len(parts) == 1 {
		return parts[0], "simple"
	}
	return parts[0], parts[1]
}

func canonicalizeBody(body, mode string) []byte {
	lines := splitLines(strings.ReplaceAll(body, "\r\n", "\n"))
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
		if mode == "relaxed" {
			lines[i] = collapseWSP(strings.TrimRight(lines[i], " \t"))
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []byte("\r\n")
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

func buildSignedData(headers []Header, hTag, dkimSigValue, canon string) (string, error) {
	headerNames := strings.Split(strings.ToLower(hTag), ":")
	if len(headerNames) == 0 {
		return "", fmt.Errorf("h tag missing")
	}
	used := make([]bool, len(headers))
	var out strings.Builder
	for _, hn := range headerNames {
		hn = strings.TrimSpace(hn)
		if hn == "" {
			continue
		}
		idx := findHeaderFromBottom(headers, used, hn)
		if idx < 0 {
			continue
		}
		used[idx] = true
		out.WriteString(canonHeader(headers[idx].Name, headers[idx].Value, canon))
	}
	stripped := stripBTag(dkimSigValue)
	out.WriteString(canonHeader("DKIM-Signature", stripped, canon))
	return out.String(), nil
}

func findHeaderFromBottom(headers []Header, used []bool, name string) int {
	for i := len(headers) - 1; i >= 0; i-- {
		if used[i] {
			continue
		}
		if strings.EqualFold(headers[i].Name, name) {
			return i
		}
	}
	return -1
}

func canonHeader(name, value, mode string) string {
	if mode == "relaxed" {
		return strings.ToLower(strings.TrimSpace(name)) + ":" + collapseWSP(strings.TrimSpace(value)) + "\r\n"
	}
	return name + ":" + value + "\r\n"
}

func collapseWSP(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func stripBTag(sig string) string {
	parts := strings.Split(sig, ";")
	for i, p := range parts {
		t := strings.TrimSpace(p)
		if strings.HasPrefix(strings.ToLower(t), "b=") {
			parts[i] = " b="
		}
	}
	return strings.TrimSpace(strings.Join(parts, ";"))
}

func compactBTag(v string) string {
	return strings.Join(strings.Fields(v), "")
}

func lookupDKIMKey(domain, selector string) (*rsa.PublicKey, error) {
	q := selector + "._domainkey." + domain
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	txt, err := net.DefaultResolver.LookupTXT(ctx, q)
	if err != nil {
		return nil, err
	}
	all := strings.Join(txt, "")
	tags := parseTagList(all)
	p := tags["p"]
	if p == "" {
		return nil, fmt.Errorf("missing p tag")
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
	if err != nil {
		return nil, err
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("non-rsa dkim key")
	}
	return pub, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var x byte
	for i := range a {
		x |= a[i] ^ b[i]
	}
	return x == 0
}
