package dkim2

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TXTResolver resolves a DKIM2 public-key TXT RR. The returned slice contains
// complete records, after concatenating any character-strings in each RR.
type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Verify performs bounded hash, DNS key, and cryptographic verification for
// messages without recipes. Recipe reconstruction is intentionally deferred.
func Verify(ctx context.Context, raw []byte, mailFrom string, recipients []string, limits Limits, resolver TXTResolver) Result {
	result := Evaluate(raw, mailFrom, recipients, limits)
	if result.Result != "neutral" {
		return result
	}
	if resolver == nil {
		return failed(result, "temperror", errors.New("DKIM2 key resolver is unavailable"))
	}

	headers, body, err := splitMessage(raw)
	if err != nil {
		return failed(result, "permerror", err)
	}
	instances, signatures, err := parsedDKIM2Fields(headers)
	if err != nil {
		return failed(result, "permerror", err)
	}
	for _, instance := range instances {
		if instance.tags["r"] != "" {
			return failed(result, "neutral", errors.New("recipe reconstruction not implemented"))
		}
	}
	if err := verifyMessageHashes(headers, body, instances[len(instances)-1].tags["h"]); err != nil {
		return failed(result, "fail", err)
	}

	verifyCtx, cancel := context.WithTimeout(ctx, limits.DNSTimeout)
	defer cancel()
	queries := 0
	for index, signature := range signatures {
		sets, err := parseSignatureSets(signature.tags["s"])
		if err != nil {
			return failed(result, "permerror", err)
		}
		canonical := canonicalSignatureInput(instances, signatures[:index], signature)
		for _, set := range sets {
			if queries >= limits.MaxDNSQueries {
				result.Queries = queries
				return failed(result, "temperror", errors.New("DNS query limit exceeded"))
			}
			queries++
			result.Queries = queries
			records, err := resolver.LookupTXT(verifyCtx, set.selector+"._domainkey."+signature.tags["d"])
			if err != nil {
				return failed(result, "temperror", fmt.Errorf("public key %s could not be fetched: %w", set.selector, err))
			}
			if len(records) != 1 {
				return failed(result, "permerror", fmt.Errorf("public key %s requires exactly one record", set.selector))
			}
			if err := verifySignature(canonical, set, records[0]); err != nil {
				state := "permerror"
				if errors.Is(err, errIncorrectSignature) {
					state = "fail"
				}
				return failed(result, state, fmt.Errorf("selector %s: %w", set.selector, err))
			}
		}
	}
	result.Result, result.Reason = "pass", ""
	return result
}

type signatureSet struct {
	selector  string
	algorithm string
	value     []byte
}

var errIncorrectSignature = errors.New("incorrect signature")

func parseSignatureSets(value string) ([]signatureSet, error) {
	seen := map[string]bool{}
	var sets []signatureSet
	for _, encoded := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(encoded), ":")
		if len(parts) != 3 || parts[0] == "" || seen[parts[0]] {
			return nil, errors.New("invalid or duplicate DKIM2 selector")
		}
		seen[parts[0]] = true
		signature, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(parts[2]), ""))
		if err != nil || len(signature) == 0 {
			return nil, errors.New("invalid DKIM2 signature encoding")
		}
		sets = append(sets, signatureSet{selector: parts[0], algorithm: parts[1], value: signature})
	}
	return sets, nil
}

func splitMessage(raw []byte) ([]field, []byte, error) {
	separator := strings.Index(string(raw), "\r\n\r\n")
	if separator < 0 {
		return nil, nil, errors.New("missing CRLF header separator")
	}
	headers, err := parseHeaders(raw)
	if err != nil {
		return nil, nil, err
	}
	return headers, raw[separator+4:], nil
}

func parsedDKIM2Fields(headers []field) ([]field, []field, error) {
	var instances, signatures []field
	for _, header := range headers {
		var numberTag string
		switch strings.ToLower(header.name) {
		case "message-instance":
			numberTag = "m"
		case "dkim2-signature":
			numberTag = "i"
		default:
			continue
		}
		parsed, err := parseField(header, numberTag)
		if err != nil {
			return nil, nil, err
		}
		if numberTag == "m" {
			instances = append(instances, parsed)
		} else {
			signatures = append(signatures, parsed)
		}
	}
	return instances, signatures, nil
}

func verifyMessageHashes(headers []field, body []byte, value string) error {
	canonicalHeaders := canonicalMessageHeaders(headers)
	canonicalBody := canonicalBody(body)
	seen := map[string]bool{}
	for _, hashSet := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(hashSet), ":")
		if len(parts) != 3 || seen[parts[0]] {
			return errors.New("invalid or duplicate Message-Instance hash")
		}
		seen[parts[0]] = true
		var headerHash, bodyHash []byte
		switch parts[0] {
		case "sha256":
			h := sha256.Sum256(canonicalHeaders)
			b := sha256.Sum256(canonicalBody)
			headerHash, bodyHash = h[:], b[:]
		case "sha512":
			h := sha512.Sum512(canonicalHeaders)
			b := sha512.Sum512(canonicalBody)
			headerHash, bodyHash = h[:], b[:]
		default:
			return fmt.Errorf("unsupported Message-Instance hash %s", parts[0])
		}
		if base64.StdEncoding.EncodeToString(headerHash) != parts[1] {
			return errors.New("Message-Instance header hash mismatch")
		}
		if base64.StdEncoding.EncodeToString(bodyHash) != parts[2] {
			return errors.New("Message-Instance body hash mismatch")
		}
	}
	return nil
}

func canonicalBody(body []byte) []byte {
	value := strings.TrimSuffix(string(body), "\r\n")
	for strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	}
	return []byte(value + "\r\n")
}

func canonicalMessageHeaders(headers []field) []byte {
	grouped := map[string][]string{}
	for _, header := range headers {
		name := strings.ToLower(header.name)
		if ignoredHeader(name) || name == "message-instance" || name == "dkim2-signature" {
			continue
		}
		value := strings.Join(strings.Fields(header.value), " ")
		grouped[name] = append(grouped[name], name+":"+strings.TrimRight(value, " \t")+"\r\n")
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	var output strings.Builder
	for _, name := range names {
		values := grouped[name]
		for index := len(values) - 1; index >= 0; index-- {
			output.WriteString(values[index])
		}
	}
	return []byte(output.String())
}

func ignoredHeader(name string) bool {
	if strings.HasPrefix(name, "received-") || strings.HasPrefix(name, "x-") {
		return true
	}
	switch name {
	case "apparently-to", "arc-authentication-results", "arc-message-signature", "arc-seal", "authentication-results", "auto-submitted", "delivered-to", "dkim-signature", "dl-expansion-history", "original-recipient", "received", "return-path", "sio-label-history", "vbr-info", "x400-received", "x400-trace":
		return true
	default:
		return false
	}
}

func canonicalSignatureInput(instances, previous []field, current field) []byte {
	var output strings.Builder
	for _, instance := range instances {
		output.WriteString(canonicalDKIM2Field(instance.name, instance.value))
	}
	for _, signature := range previous {
		output.WriteString(canonicalDKIM2Field(signature.name, signature.value))
	}
	output.WriteString(canonicalDKIM2Field(current.name, blankSignatures(current.value)))
	return []byte(output.String())
}

func canonicalDKIM2Field(name, value string) string {
	value = strings.NewReplacer(" ", "", "\t", "").Replace(value)
	return strings.ToLower(name) + ":" + value + "\r\n"
}

func blankSignatures(value string) string {
	parts := strings.Split(value, ";")
	for index, part := range parts {
		trimmed := strings.TrimSpace(part)
		if !strings.HasPrefix(trimmed, "s=") {
			continue
		}
		sets := strings.Split(strings.TrimPrefix(trimmed, "s="), ",")
		for setIndex, set := range sets {
			fields := strings.Split(set, ":")
			if len(fields) == 3 {
				fields[2] = ""
				sets[setIndex] = strings.Join(fields, ":")
			}
		}
		parts[index] = "s=" + strings.Join(sets, ",")
	}
	return strings.Join(parts, ";")
}

func verifySignature(input []byte, set signatureSet, record string) error {
	keyType, keyData, err := parseKeyRecord(record)
	if err != nil {
		return err
	}
	switch set.algorithm {
	case "rsa-sha256":
		if keyType != "rsa" {
			return errors.New("public key algorithm mismatch")
		}
		publicKey, err := x509.ParsePKIXPublicKey(keyData)
		if err != nil {
			if parsed, parseErr := x509.ParsePKCS1PublicKey(keyData); parseErr == nil {
				publicKey = parsed
			} else {
				return errors.New("invalid RSA public key")
			}
		}
		rsaKey, ok := publicKey.(*rsa.PublicKey)
		if !ok || rsaKey.N.BitLen() < 1024 || rsaKey.E != 65537 {
			return errors.New("invalid RSA public key")
		}
		digest := sha256.Sum256(input)
		if err := rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, digest[:], set.value); err != nil {
			return errIncorrectSignature
		}
	case "ed25519-sha256":
		if keyType != "ed25519" || len(keyData) != ed25519.PublicKeySize {
			return errors.New("public key algorithm mismatch")
		}
		digest := sha256.Sum256(input)
		if !ed25519.Verify(ed25519.PublicKey(keyData), digest[:], set.value) {
			return errIncorrectSignature
		}
	default:
		return fmt.Errorf("unsupported signature algorithm %s", set.algorithm)
	}
	return nil
}

func parseKeyRecord(record string) (string, []byte, error) {
	tags := map[string]string{}
	for index, part := range strings.Split(record, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		at := strings.IndexByte(part, '=')
		if at <= 0 {
			return "", nil, errors.New("public key syntax error")
		}
		name, value := strings.TrimSpace(part[:at]), strings.TrimSpace(part[at+1:])
		if _, exists := tags[name]; exists {
			return "", nil, errors.New("duplicate public key tag")
		}
		if name == "v" && (index != 0 || value != "DKIM1") {
			return "", nil, errors.New("invalid public key version")
		}
		tags[name] = value
	}
	keyType := tags["k"]
	if keyType == "" {
		keyType = "rsa"
	}
	if tags["p"] == "" {
		return "", nil, errors.New("public key is absent or revoked")
	}
	keyData, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(tags["p"]), ""))
	if err != nil {
		return "", nil, errors.New("invalid public key encoding")
	}
	return keyType, keyData, nil
}
