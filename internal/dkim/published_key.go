package dkim

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"strings"
)

// Activation checks one TXT RR from the configured resolver's view. It cannot
// prove propagation to all receivers; operators must wait out DNS caches.
func publishedKeyMatches(records []string, public crypto.PublicKey, algorithm string) bool {
	if len(records) != 1 || len(records[0]) > 16*1024 {
		return false
	}
	tags := map[string]string{}
	for i, part := range strings.Split(records[0], ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return false
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "v" && i != 0 {
			return false
		}
		if _, duplicate := tags[key]; duplicate {
			return false
		}
		tags[key] = value
	}
	if v, present := tags["v"]; present && v != "DKIM1" {
		return false
	}
	if h, present := tags["h"]; present && !containsKeyTag(h, "sha256") {
		return false
	}
	if service, present := tags["s"]; present && !containsKeyTag(service, "*") && !containsKeyTag(service, "email") {
		return false
	}
	encoded := strings.Join(strings.Fields(tags["p"]), "")
	if encoded == "" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	switch algorithm {
	case "ed25519-sha256":
		key, ok := public.(ed25519.PublicKey)
		return ok && tags["k"] == "ed25519" && len(raw) == ed25519.PublicKeySize && bytes.Equal(raw, key)
	case "rsa-sha256":
		if kind, present := tags["k"]; present && kind != "rsa" {
			return false
		}
		expected, ok := public.(*rsa.PublicKey)
		if !ok {
			return false
		}
		parsed, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			parsed, err = x509.ParsePKCS1PublicKey(raw)
		}
		if err != nil {
			return false
		}
		actual, ok := parsed.(*rsa.PublicKey)
		return ok && actual.E == expected.E && actual.N.Cmp(expected.N) == 0
	default:
		return false
	}
}

func containsKeyTag(value, want string) bool {
	for _, part := range strings.Split(value, ":") {
		if strings.TrimSpace(part) == want {
			return true
		}
	}
	return false
}
