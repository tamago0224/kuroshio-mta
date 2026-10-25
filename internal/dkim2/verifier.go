// Package dkim2 contains an observation-only verifier experiment.
// It targets draft-ietf-dkim-dkim2-spec-07 and must not make acceptance decisions.
package dkim2

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	// SpecificationRevision is the exact signature draft implemented by this experiment.
	SpecificationRevision = "draft-ietf-dkim-dkim2-spec-07"
	// DNSRevision is the exact DNS draft tracked by this experiment.
	DNSRevision = "draft-ietf-dkim-dkim2-dns-02"
	// BCPRevision is the exact operational draft tracked by this experiment.
	BCPRevision = "draft-ietf-dkim-dkim2-bcp-01"
)

// Limits bounds work performed on untrusted DKIM2 input.
type Limits struct {
	MaxMessageBytes int
	MaxChain        int
	MaxRecipeBytes  int
	MaxRecipeDepth  int
	MaxDNSQueries   int
	DNSTimeout      time.Duration
}

// DefaultLimits returns conservative limits for the observation-only verifier.
func DefaultLimits() Limits {
	return Limits{MaxMessageBytes: 10 << 20, MaxChain: 16, MaxRecipeBytes: 64 << 10, MaxRecipeDepth: 16, MaxDNSQueries: 8, DNSTimeout: 2 * time.Second}
}

// Result describes an observation without affecting message acceptance.
type Result struct {
	Revision  string `json:"revision"`
	Result    string `json:"result"`
	Reason    string `json:"reason,omitempty"`
	Instances int    `json:"instances"`
	Chain     int    `json:"chain"`
	Queries   int    `json:"queries"`
}

type field struct {
	name   string
	value  string
	tags   map[string]string
	number int
}

// Evaluate performs bounded structural validation. It does not verify hashes or
// signatures, so structurally valid input is reported as neutral rather than pass.
func Evaluate(raw []byte, mailFrom string, recipients []string, limits Limits) Result {
	result := Result{Revision: SpecificationRevision, Result: "none"}
	if err := validateLimits(limits); err != nil {
		return failed(result, "permerror", err)
	}
	if len(raw) > limits.MaxMessageBytes {
		return failed(result, "permerror", errors.New("message size limit exceeded"))
	}
	headers, err := parseHeaders(raw)
	if err != nil {
		return failed(result, "permerror", err)
	}
	instances, signatures := []field{}, []field{}
	for _, header := range headers {
		switch strings.ToLower(header.name) {
		case "message-instance":
			instances = append(instances, header)
		case "dkim2-signature":
			signatures = append(signatures, header)
		}
	}
	if len(instances) == 0 && len(signatures) == 0 {
		return result
	}
	result.Result, result.Instances, result.Chain = "neutral", len(instances), len(signatures)
	if len(instances) == 0 || len(signatures) == 0 {
		return failed(result, "permerror", errors.New("incomplete DKIM2 header set"))
	}
	if len(instances) > limits.MaxChain || len(signatures) > limits.MaxChain {
		return failed(result, "permerror", errors.New("chain depth limit exceeded"))
	}
	for index := range instances {
		instances[index], err = parseField(instances[index], "m")
		if err != nil {
			return failed(result, "permerror", err)
		}
		if instances[index].number != index+1 {
			return failed(result, "permerror", errors.New("Message-Instance chain gap or duplicate"))
		}
		if err := validateInstanceTags(instances[index].tags); err != nil {
			return failed(result, "permerror", err)
		}
		if recipe := instances[index].tags["r"]; recipe != "" {
			if err := validateRecipe(recipe, limits); err != nil {
				return failed(result, "permerror", err)
			}
		}
	}
	for index := range signatures {
		signatures[index], err = parseField(signatures[index], "i")
		if err != nil {
			return failed(result, "permerror", err)
		}
		if signatures[index].number != index+1 {
			return failed(result, "permerror", errors.New("DKIM2-Signature chain gap or duplicate"))
		}
		if err := validateSignatureTags(signatures[index].tags, len(instances)); err != nil {
			return failed(result, "permerror", err)
		}
	}
	if err := validateEnvelope(signatures[len(signatures)-1].tags, mailFrom, recipients); err != nil {
		return failed(result, "fail", err)
	}
	result.Reason = "structural checks passed; cryptographic verification not performed"
	return result
}

func validateLimits(l Limits) error {
	if l.MaxMessageBytes < 1 || l.MaxChain < 1 || l.MaxRecipeBytes < 1 || l.MaxRecipeDepth < 1 || l.MaxDNSQueries < 1 || l.DNSTimeout <= 0 {
		return errors.New("invalid DKIM2 resource limits")
	}
	return nil
}

func parseHeaders(raw []byte) ([]field, error) {
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return nil, errors.New("missing CRLF header separator")
	}
	lines := strings.Split(string(raw[:end]), "\r\n")
	out := []field{}
	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(out) == 0 {
				return nil, errors.New("orphan header continuation")
			}
			out[len(out)-1].value += " " + strings.TrimSpace(line)
			continue
		}
		at := strings.IndexByte(line, ':')
		if at <= 0 {
			return nil, errors.New("malformed header")
		}
		out = append(out, field{name: line[:at], value: strings.TrimSpace(line[at+1:])})
	}
	return out, nil
}

func parseField(input field, numberTag string) (field, error) {
	tags := map[string]string{}
	for _, part := range strings.Split(input.value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		at := strings.IndexByte(part, '=')
		if at <= 0 {
			return input, errors.New("malformed DKIM2 tag")
		}
		name := strings.TrimSpace(part[:at])
		if name != strings.ToLower(name) {
			return input, errors.New("DKIM2 tag names must be lowercase")
		}
		if _, exists := tags[name]; exists {
			return input, fmt.Errorf("duplicate DKIM2 tag %q", name)
		}
		tags[name] = strings.TrimSpace(part[at+1:])
	}
	number, err := strconv.Atoi(tags[numberTag])
	if err != nil || number < 1 {
		return input, fmt.Errorf("invalid %s tag", numberTag)
	}
	input.tags, input.number = tags, number
	return input, nil
}

func validateInstanceTags(tags map[string]string) error {
	value := strings.Split(tags["h"], ":")
	if len(value) != 3 || value[0] != "sha256" {
		return errors.New("invalid Message-Instance h tag")
	}
	for _, digest := range value[1:] {
		if _, err := base64.StdEncoding.DecodeString(digest); err != nil {
			return errors.New("invalid Message-Instance h tag")
		}
	}
	return nil
}

func validateSignatureTags(tags map[string]string, instances int) error {
	for _, name := range []string{"i", "m", "t", "d", "s"} {
		if tags[name] == "" {
			return fmt.Errorf("DKIM2-Signature missing %s tag", name)
		}
	}
	m, err := strconv.Atoi(tags["m"])
	if err != nil || m < 1 || m > instances {
		return errors.New("invalid DKIM2-Signature m tag")
	}
	timestamp, err := strconv.ParseInt(tags["t"], 10, 64)
	if err != nil || timestamp < 0 {
		return errors.New("invalid DKIM2-Signature timestamp")
	}
	now := time.Now()
	signed := time.Unix(timestamp, 0)
	if signed.Before(now.Add(-14*24*time.Hour)) || signed.After(now.Add(24*time.Hour)) {
		return errors.New("DKIM2-Signature timestamp outside allowed window")
	}
	hasND, hasMF, hasRT := tags["nd"] != "", tags["mf"] != "", tags["rt"] != ""
	if hasND == (hasMF || hasRT) || (!hasND && !(hasMF && hasRT)) {
		return errors.New("DKIM2-Signature requires nd or mf and rt")
	}
	if len(tags["n"]) > 64 {
		return errors.New("DKIM2 nonce too long")
	}
	signature := strings.Split(tags["s"], ":")
	if len(signature) != 3 || signature[0] == "" || signature[2] == "" {
		return errors.New("invalid DKIM2-Signature s tag")
	}
	switch signature[1] {
	case "rsa-sha256", "ed25519-sha256", "mldsa44-ed25519-sha512":
	default:
		return errors.New("unsupported DKIM2 signature algorithm")
	}
	if _, err := base64.StdEncoding.DecodeString(signature[2]); err != nil {
		return errors.New("invalid DKIM2 signature encoding")
	}
	return nil
}

func validateEnvelope(tags map[string]string, mailFrom string, recipients []string) error {
	if tags["nd"] != "" {
		return errors.New("highest DKIM2 signature uses nd without out-of-band arrangement")
	}
	mf, err := decodePath(tags["mf"])
	if err != nil {
		return errors.New("invalid mf tag")
	}
	wantMF := "<>"
	if strings.TrimSpace(mailFrom) != "" {
		wantMF = "<" + strings.TrimSpace(mailFrom) + ">"
	}
	if !samePath(mf, wantMF) {
		return errors.New("MAIL FROM mismatch")
	}
	allowed := map[string]bool{}
	for _, encoded := range strings.Split(tags["rt"], ",") {
		path, err := decodePath(encoded)
		if err != nil {
			return errors.New("invalid rt tag")
		}
		allowed[normalizePath(path)] = true
	}
	for _, recipient := range recipients {
		if !allowed[normalizePath("<"+recipient+">")] {
			return errors.New("RCPT TO mismatch")
		}
	}
	return nil
}

func decodePath(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
	return string(decoded), err
}

func normalizePath(value string) string {
	at := strings.LastIndex(value, "@")
	if at < 0 {
		return value
	}
	return value[:at+1] + strings.ToLower(value[at+1:])
}

func samePath(a, b string) bool { return normalizePath(a) == normalizePath(b) }

func validateRecipe(encoded string, limits Limits) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil {
		return errors.New("invalid recipe base64")
	}
	if len(decoded) > limits.MaxRecipeBytes {
		return errors.New("recipe size limit exceeded")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	if err := decoder.Decode(&value); err != nil {
		return errors.New("invalid recipe JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid recipe JSON")
	}
	if depth(value) > limits.MaxRecipeDepth {
		return errors.New("recipe depth limit exceeded")
	}
	object, ok := value.(map[string]any)
	if !ok || (object["h"] == nil && object["b"] == nil) {
		return errors.New("recipe requires h or b")
	}
	return nil
}

func depth(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		maxDepth := 1
		for _, child := range typed {
			maxDepth = max(maxDepth, 1+depth(child))
		}
		return maxDepth
	case []any:
		maxDepth := 1
		for _, child := range typed {
			maxDepth = max(maxDepth, 1+depth(child))
		}
		return maxDepth
	default:
		return 0
	}
}

func failed(result Result, state string, err error) Result {
	result.Result, result.Reason = state, err.Error()
	return result
}
