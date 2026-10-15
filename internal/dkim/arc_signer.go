package dkim

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ARCFileSigner struct {
	domain   string
	selector string
	authserv string
	headers  []string
	keyFile  string

	mu         sync.RWMutex
	mtime      time.Time
	privateKey *rsa.PrivateKey
}

func NewARCFileSigner(domain, selector, keyFile, authservID, headers string) (*ARCFileSigner, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	s := strings.TrimSpace(selector)
	k := strings.TrimSpace(keyFile)
	a := strings.TrimSpace(authservID)
	if d == "" || s == "" || k == "" {
		return nil, errors.New("arc signer requires domain, selector, and private key file")
	}
	if a == "" {
		a = "kuroshio.local"
	}
	h := parseHeaderList(headers)
	if len(h) == 0 {
		h = []string{"from", "to", "subject", "date", "message-id"}
	}
	signer := &ARCFileSigner{
		domain:   d,
		selector: s,
		authserv: a,
		headers:  h,
		keyFile:  k,
	}
	if err := signer.reloadIfNeeded(); err != nil {
		return nil, err
	}
	return signer, nil
}

// Sign seals a new message. Existing chains are conservatively marked failed
// unless the caller supplies a trusted result with SignWithChainValidation.
func (s *ARCFileSigner) Sign(raw []byte) ([]byte, error) {
	return s.SignWithChainValidation(raw, "fail")
}

// SignWithChainValidation seals raw using a cryptographically verified chain result.
// The result must come from a validator, never from an untrusted message header.
func (s *ARCFileSigner) SignWithChainValidation(raw []byte, validation string) ([]byte, error) {
	if err := s.reloadIfNeeded(); err != nil {
		return nil, err
	}
	headerPart, bodyPart, ok := splitMessage(raw)
	if !ok {
		return nil, errors.New("invalid message format")
	}
	headers := parseRawHeaders(headerPart)
	if len(headers) == 0 {
		return nil, errors.New("missing headers")
	}
	chain := inspectARCChain(headers)
	if chain.stop {
		return raw, nil
	}
	cv := "none"
	if chain.max > 0 {
		cv = "fail"
		if chain.valid && validation == "pass" {
			cv = "pass"
		}
	}
	instance := chain.max + 1
	aarValue := s.buildAARValue(headers, instance, cv)

	// AMS must not cover Authentication-Results or any ARC header (RFC 8617 4.1.2).
	want := make([]string, 0, len(s.headers))
	for _, name := range s.headers {
		if name != "authentication-results" && !isARCHeader(name) {
			want = append(want, name)
		}
	}
	for _, h := range headers {
		if strings.EqualFold(h.name, "DKIM-Signature") {
			want = append(want, "dkim-signature")
		}
	}
	hdrNames, signedHeaderPart := buildSignedHeaders(headers, want)
	if len(hdrNames) == 0 {
		return nil, errors.New("no headers available for arc message signature")
	}

	bh := bodyHash(bodyPart)
	ts := time.Now().UTC().Unix()
	amsBase := fmt.Sprintf(
		"i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
		instance,
		s.domain,
		s.selector,
		ts,
		strings.Join(hdrNames, ":"),
		bh,
	)
	amsSigningData := signedHeaderPart + canonHeaderRelaxed("ARC-Message-Signature", amsBase)
	s.mu.RLock()
	key := s.privateKey
	s.mu.RUnlock()
	amsSig, err := signPKCS1v15SHA256(key, []byte(amsSigningData))
	if err != nil {
		return nil, err
	}
	amsValue := amsBase + amsSig

	sealBase := fmt.Sprintf(
		"i=%d; cv=%s; a=rsa-sha256; d=%s; s=%s; t=%d; b=",
		instance,
		cv,
		s.domain,
		s.selector,
		ts,
	)
	sealSigningData := canonHeaderRelaxed("ARC-Authentication-Results", aarValue) +
		canonHeaderRelaxed("ARC-Message-Signature", amsValue) +
		canonHeaderRelaxed("ARC-Seal", sealBase)
	if cv == "pass" {
		sealSigningData = chain.signedData() + sealSigningData
	}
	sealSig, err := signPKCS1v15SHA256(key, []byte(sealSigningData))
	if err != nil {
		return nil, err
	}
	sealValue := sealBase + sealSig

	var out strings.Builder
	out.WriteString("ARC-Seal: ")
	out.WriteString(sealValue)
	out.WriteString("\r\n")
	out.WriteString("ARC-Message-Signature: ")
	out.WriteString(amsValue)
	out.WriteString("\r\n")
	out.WriteString("ARC-Authentication-Results: ")
	out.WriteString(aarValue)
	out.WriteString("\r\n")
	out.Write(raw)
	return []byte(out.String()), nil
}

func (s *ARCFileSigner) reloadIfNeeded() error {
	st, err := os.Stat(s.keyFile)
	if err != nil {
		return err
	}
	s.mu.RLock()
	needs := s.privateKey == nil || st.ModTime().After(s.mtime)
	s.mu.RUnlock()
	if !needs {
		return nil
	}
	key, err := loadPrivateKey(s.keyFile)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.privateKey = key
	s.mtime = st.ModTime()
	s.mu.Unlock()
	return nil
}

func (s *ARCFileSigner) buildAARValue(headers []rawHeader, instance int, cv string) string {
	for _, h := range headers {
		if strings.EqualFold(h.name, "Authentication-Results") {
			v := collapseWSP(strings.TrimSpace(h.value))
			// Only copy results from our own authserv-id. Never attest to a remote one.
			authserv, _, _ := strings.Cut(v, ";")
			if strings.TrimSpace(authserv) == s.authserv {
				parts := strings.Split(v, ";")
				kept := []string{fmt.Sprintf("i=%d", instance), s.authserv}
				for _, part := range parts[1:] {
					part = strings.TrimSpace(part)
					method, _, _ := strings.Cut(part, "=")
					if strings.TrimSpace(method) != "arc" && part != "" {
						kept = append(kept, part)
					}
				}
				return strings.Join(append(kept, "arc="+cv), "; ")
			}
		}
	}
	return fmt.Sprintf("i=%d; %s; arc=%s", instance, s.authserv, cv)
}

type arcChainState struct {
	max   int
	valid bool
	stop  bool
	sets  map[int]map[string]rawHeader
}

func isARCHeader(name string) bool {
	switch strings.ToLower(name) {
	case "arc-seal", "arc-message-signature", "arc-authentication-results":
		return true
	default:
		return false
	}
}

func arcTag(value, name string) string {
	for _, part := range strings.Split(value, ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func inspectARCChain(headers []rawHeader) arcChainState {
	state := arcChainState{valid: true, sets: make(map[int]map[string]rawHeader)}
	for _, h := range headers {
		if !isARCHeader(h.name) {
			continue
		}
		first, _, _ := strings.Cut(h.value, ";")
		instance, err := strconv.Atoi(arcTag(first, "i"))
		if err != nil || instance < 1 || instance >= 50 {
			// An ambiguous instance or exhausted chain cannot be extended safely.
			state.stop = true
			return state
		}
		if instance > state.max {
			state.max = instance
		}
		if state.sets[instance] == nil {
			state.sets[instance] = make(map[string]rawHeader)
		}
		name := strings.ToLower(h.name)
		if _, exists := state.sets[instance][name]; exists {
			state.valid = false
		}
		state.sets[instance][name] = h
	}
	for i := 1; i <= state.max; i++ {
		set := state.sets[i]
		if len(set) != 3 {
			state.valid = false
		}
		cv := arcTag(set["arc-seal"].value, "cv")
		if i == state.max && cv == "fail" {
			state.stop = true
		}
		if (i == 1 && cv != "none") || (i > 1 && cv != "pass") {
			state.valid = false
		}
	}
	return state
}

func (state arcChainState) signedData() string {
	var out strings.Builder
	for i := 1; i <= state.max; i++ {
		for _, name := range []string{"arc-authentication-results", "arc-message-signature", "arc-seal"} {
			h := state.sets[i][name]
			out.WriteString(canonHeaderRelaxed(h.name, h.value))
		}
	}
	return out.String()
}

func signPKCS1v15SHA256(key *rsa.PrivateKey, payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}
