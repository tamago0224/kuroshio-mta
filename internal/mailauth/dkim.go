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
	"strconv"
	"strings"
	"time"
)

var dkimLookupTXT = func(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func EvalDKIM(headers []Header, body string) DKIMResult {
	dkims := HeaderValues(headers, "DKIM-Signature")
	if len(dkims) == 0 {
		return DKIMResult{Result: "none"}
	}
	results := make([]DKIMSigResult, 0, len(dkims))
	for _, sig := range dkims {
		r := verifyDKIMSig(headers, body, sig)
		results = append(results, r)
	}
	return DKIMResult{Result: aggregateDKIMResults(results), Sigs: results}
}

func aggregateDKIMResults(results []DKIMSigResult) string {
	if len(results) == 0 {
		return "none"
	}
	hasPermerror := false
	hasTemperror := false
	hasNeutral := false
	for _, r := range results {
		switch strings.ToLower(strings.TrimSpace(r.Result)) {
		case "pass":
			return "pass"
		case "temperror":
			hasTemperror = true
		case "permerror":
			hasPermerror = true
		case "none":
			hasNeutral = true
		}
	}
	if hasTemperror {
		return "temperror"
	}
	if hasPermerror {
		return "permerror"
	}
	if hasNeutral {
		return "none"
	}
	return "fail"
}

func verifyDKIMSig(headers []Header, body, sig string) DKIMSigResult {
	tags := parseTagList(sig)
	domain := strings.ToLower(tags["d"])
	selector := tags["s"]
	algo := strings.ToLower(tags["a"])
	if tags["v"] != "1" || domain == "" || selector == "" || algo != "rsa-sha256" {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "unsupported signature parameters"}
	}
	if strings.TrimSpace(tags["h"]) == "" {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "missing h tag"}
	}
	if strings.TrimSpace(tags["bh"]) == "" {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "missing bh tag"}
	}
	if strings.TrimSpace(tags["b"]) == "" {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: "missing b tag"}
	}
	if err := validateDKIMTimeTags(tags, time.Now().UTC()); err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: err.Error()}
	}
	canonH, canonB := parseCanon(tags["c"])

	canonBody, err := canonicalizeBodyWithLength(body, canonB, tags["l"])
	if err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: err.Error()}
	}
	h := sha256.Sum256(canonBody)
	expectedBH, err := base64.StdEncoding.DecodeString(strings.TrimSpace(tags["bh"]))
	if err != nil || !equalBytes(h[:], expectedBH) {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "fail", Reason: "body hash mismatch"}
	}

	signedData, err := buildSignedData(headers, tags["h"], sig, canonH, "DKIM-Signature")
	if err != nil {
		return DKIMSigResult{Domain: domain, Selector: selector, Result: "permerror", Reason: err.Error()}
	}
	pub, err := lookupDKIMKey(domain, selector)
	if err != nil {
		if lerr, ok := err.(*dkimLookupError); ok {
			return DKIMSigResult{Domain: domain, Selector: selector, Result: lerr.Result, Reason: lerr.Reason}
		}
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
	out, _ := canonicalizeBodyWithLength(body, mode, "")
	return out
}

func canonicalizeBodyWithLength(body, mode, bodyLength string) ([]byte, error) {
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
		return []byte("\r\n"), nil
	}
	out := []byte(strings.Join(lines, "\r\n") + "\r\n")
	if strings.TrimSpace(bodyLength) == "" {
		return out, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(bodyLength))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid l tag")
	}
	if n > len(out) {
		return nil, fmt.Errorf("invalid l tag")
	}
	return out[:n], nil
}

func buildSignedData(headers []Header, hTag, sigValue, canon, sigHeaderName string) (string, error) {
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
	stripped := stripBTag(sigValue)
	out.WriteString(canonHeader(sigHeaderName, stripped, canon))
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
	txt, err := dkimLookupTXT(ctx, q)
	if err != nil {
		return nil, err
	}
	all := strings.Join(txt, "")
	tags := parseTagList(all)
	p := tags["p"]
	if p == "" {
		return nil, &dkimLookupError{Result: "permerror", Reason: "missing p tag"}
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
	if err != nil {
		return nil, &dkimLookupError{Result: "permerror", Reason: "invalid p tag"}
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, &dkimLookupError{Result: "permerror", Reason: "invalid public key"}
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, &dkimLookupError{Result: "permerror", Reason: "non-rsa dkim key"}
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

type dkimLookupError struct {
	Result string
	Reason string
}

func (e *dkimLookupError) Error() string {
	return e.Reason
}

func validateDKIMTimeTags(tags map[string]string, now time.Time) error {
	tVal := strings.TrimSpace(tags["t"])
	xVal := strings.TrimSpace(tags["x"])
	var (
		tUnix int64
		xUnix int64
		hasT  bool
	)
	if tVal != "" {
		n, err := strconv.ParseInt(tVal, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid t tag")
		}
		tUnix = n
		hasT = true
	}
	if xVal != "" {
		n, err := strconv.ParseInt(xVal, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid x tag")
		}
		xUnix = n
		if hasT && xUnix < tUnix {
			return fmt.Errorf("x tag is earlier than t tag")
		}
		if now.Unix() > xUnix {
			return fmt.Errorf("signature expired")
		}
	}
	return nil
}
