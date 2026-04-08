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

func (s *ARCFileSigner) Sign(raw []byte) ([]byte, error) {
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
	if hasARCSet(headers) {
		// Existing chain update is handled by the next issue; keep current message as-is.
		return raw, nil
	}

	aarValue := s.buildAARValue(headers)

	msgWithAAR := "ARC-Authentication-Results: " + aarValue + "\r\n" + headerPart + "\r\n\r\n" + bodyPart
	aarHeaderPart, _, _ := splitMessage([]byte(msgWithAAR))
	headersWithAAR := parseRawHeaders(aarHeaderPart)

	hdrNames, signedHeaderPart := buildSignedHeaders(headersWithAAR, append(append([]string{}, s.headers...), "arc-authentication-results"))
	if len(hdrNames) == 0 {
		return nil, errors.New("no headers available for arc message signature")
	}

	bh := bodyHash(bodyPart)
	ts := time.Now().UTC().Unix()
	amsBase := fmt.Sprintf(
		"i=1; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
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

	sealBase := fmt.Sprintf("i=1; cv=none; a=rsa-sha256; d=%s; s=%s; t=%d; b=", s.domain, s.selector, ts)
	sealSigningData := canonHeaderRelaxed("ARC-Authentication-Results", aarValue) +
		canonHeaderRelaxed("ARC-Message-Signature", amsValue) +
		canonHeaderRelaxed("ARC-Seal", sealBase)
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
	out.WriteString(headerPart)
	out.WriteString("\r\n\r\n")
	out.WriteString(bodyPart)
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

func (s *ARCFileSigner) buildAARValue(headers []rawHeader) string {
	for _, h := range headers {
		if strings.EqualFold(h.name, "Authentication-Results") {
			v := collapseWSP(strings.TrimSpace(h.value))
			if v != "" {
				return "i=1; " + v
			}
		}
	}
	return fmt.Sprintf("i=1; %s; arc=none", s.authserv)
}

func hasARCSet(headers []rawHeader) bool {
	for _, h := range headers {
		if strings.EqualFold(h.name, "ARC-Seal") || strings.EqualFold(h.name, "ARC-Message-Signature") || strings.EqualFold(h.name, "ARC-Authentication-Results") {
			return true
		}
	}
	return false
}

func signPKCS1v15SHA256(key *rsa.PrivateKey, payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}
