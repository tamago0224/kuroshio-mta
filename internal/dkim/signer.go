package dkim

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type FileSigner struct {
	domain   string
	selector string
	headers  []string
	keyFile  string

	mu      sync.RWMutex
	mtime   time.Time
	privKey *rsa.PrivateKey
}

func NewFileSigner(domain, selector, keyFile, headers string) (*FileSigner, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	s := strings.TrimSpace(selector)
	k := strings.TrimSpace(keyFile)
	if d == "" || s == "" || k == "" {
		return nil, errors.New("dkim signer requires domain, selector, and private key file")
	}
	h := parseHeaderList(headers)
	if len(h) == 0 {
		h = []string{"from", "to", "subject", "date", "message-id"}
	}
	f := &FileSigner{
		domain:   d,
		selector: s,
		headers:  h,
		keyFile:  k,
	}
	if err := f.reloadIfNeeded(); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *FileSigner) Sign(raw []byte) ([]byte, error) {
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
	signedHeaders, canonHeaderPart := buildSignedHeaders(headers, s.headers)
	if len(signedHeaders) == 0 {
		return nil, errors.New("no headers available for dkim signing")
	}

	bh := bodyHash(bodyPart)
	base := fmt.Sprintf(
		"v=1; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
		s.domain,
		s.selector,
		time.Now().UTC().Unix(),
		strings.Join(signedHeaders, ":"),
		bh,
	)

	signingData := canonHeaderPart + canonHeaderRelaxed("DKIM-Signature", base)
	sum := sha256.Sum256([]byte(signingData))
	s.mu.RLock()
	key := s.privKey
	s.mu.RUnlock()
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return nil, err
	}
	dkimHeader := "DKIM-Signature: " + base + base64.StdEncoding.EncodeToString(sig)
	var out strings.Builder
	out.WriteString(dkimHeader)
	out.WriteString("\r\n")
	out.WriteString(headerPart)
	out.WriteString("\r\n\r\n")
	out.WriteString(bodyPart)
	return []byte(out.String()), nil
}

func (s *FileSigner) reloadIfNeeded() error {
	st, err := os.Stat(s.keyFile)
	if err != nil {
		return err
	}
	s.mu.RLock()
	needs := s.privKey == nil || st.ModTime().After(s.mtime)
	s.mu.RUnlock()
	if !needs {
		return nil
	}
	key, err := loadPrivateKey(s.keyFile)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.privKey = key
	s.mtime = st.ModTime()
	s.mu.Unlock()
	return nil
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("invalid pem private key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := keyAny.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key is not rsa")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

type rawHeader struct {
	name  string
	value string
}

func splitMessage(raw []byte) (headers string, body string, ok bool) {
	s := string(raw)
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 {
		return s[:i], s[i+4:], true
	}
	if i := strings.Index(s, "\n\n"); i >= 0 {
		return s[:i], s[i+2:], true
	}
	return "", "", false
}

func parseRawHeaders(part string) []rawHeader {
	lines := strings.Split(strings.ReplaceAll(part, "\r\n", "\n"), "\n")
	out := make([]rawHeader, 0, len(lines))
	var curName string
	var curVal strings.Builder
	flush := func() {
		if curName == "" {
			return
		}
		out = append(out, rawHeader{name: curName, value: curVal.String()})
		curName = ""
		curVal.Reset()
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if curName != "" {
				curVal.WriteByte(' ')
				curVal.WriteString(strings.TrimLeft(line, " \t"))
			}
			continue
		}
		flush()
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		curName = strings.TrimSpace(line[:i])
		curVal.WriteString(strings.TrimSpace(line[i+1:]))
	}
	flush()
	return out
}

func buildSignedHeaders(headers []rawHeader, want []string) ([]string, string) {
	used := make([]bool, len(headers))
	names := make([]string, 0, len(want))
	var b strings.Builder
	for _, hn := range want {
		target := strings.ToLower(strings.TrimSpace(hn))
		if target == "" {
			continue
		}
		for i := len(headers) - 1; i >= 0; i-- {
			if used[i] {
				continue
			}
			if strings.EqualFold(headers[i].name, target) {
				used[i] = true
				names = append(names, target)
				b.WriteString(canonHeaderRelaxed(headers[i].name, headers[i].value))
				break
			}
		}
	}
	return names, b.String()
}

func canonHeaderRelaxed(name, value string) string {
	return strings.ToLower(strings.TrimSpace(name)) + ":" + collapseWSP(strings.TrimSpace(value)) + "\r\n"
}

func collapseWSP(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func bodyHash(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = collapseWSP(strings.TrimRight(lines[i], " \t"))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	canon := "\r\n"
	if len(lines) > 0 {
		canon = strings.Join(lines, "\r\n") + "\r\n"
	}
	sum := sha256.Sum256([]byte(canon))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func parseHeaderList(v string) []string {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		x := strings.ToLower(strings.TrimSpace(p))
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
