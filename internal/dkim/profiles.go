package dkim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Profile is an administrator-controlled, exact-domain signing identity.
type Profile struct {
	Domain    string `json:"domain"`
	Selector  string `json:"selector"`
	Algorithm string `json:"algorithm"`
	KeyFile   string `json:"key_file"`
	Headers   string `json:"headers,omitempty"`
	ARC       bool   `json:"arc,omitempty"`
}

type profileManifest struct {
	SchemaVersion int       `json:"schema_version"`
	Revision      string    `json:"revision"`
	UnknownDomain string    `json:"unknown_domain"`
	Profiles      []Profile `json:"profiles"`
}

// ProfileSigners is one immutable key/selector set retained through signing.
type ProfileSigners struct {
	DKIM *FileSigner
	ARC  *ARCFileSigner
}

type profileSnapshot struct {
	revision        string
	digest          [32]byte
	unsignedUnknown bool
	signers         map[string]ProfileSigners
}

type TXTResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
}

// ProfileManager publishes immutable signing sets. Reload failures leave the
// prior set available; readers retain one key/selector pair through signing.
type ProfileManager struct {
	mu             sync.Mutex
	path, authserv string
	resolver       TXTResolver
	current        atomic.Pointer[profileSnapshot]
}

func NewProfileManager(ctx context.Context, path, authserv string, resolver TXTResolver) (*ProfileManager, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	m := &ProfileManager{path: path, authserv: authserv, resolver: resolver}
	if _, err := m.Reload(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *ProfileManager) Reload(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	next, err := loadProfiles(ctx, m.path, m.authserv, m.resolver)
	if err != nil {
		return false, err
	}
	old := m.current.Load()
	if old != nil {
		for domain, pair := range next.signers {
			previous, ok := old.signers[domain]
			if !ok || previous.DKIM.selector != pair.DKIM.selector {
				continue
			}
			prior, _ := x509.MarshalPKIXPublicKey(previous.DKIM.privKey.Public())
			candidate, _ := x509.MarshalPKIXPublicKey(pair.DKIM.privKey.Public())
			if !bytes.Equal(prior, candidate) {
				return false, errors.New("key rotation requires a new DKIM selector")
			}
		}
	}

	if old != nil && old.digest == next.digest {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.current.Store(next)
	return true, nil
}

// Signers selects an exact domain granted by trusted ingress, not a raw header.
func (m *ProfileManager) Signers(domain string) (ProfileSigners, error) {
	snapshot := m.current.Load()
	if snapshot == nil {
		return ProfileSigners{}, errors.New("signing profiles unavailable")
	}
	if signers, ok := snapshot.signers[strings.ToLower(domain)]; ok {
		return signers, nil
	}
	if snapshot.unsignedUnknown {
		return ProfileSigners{}, nil
	}
	return ProfileSigners{}, errors.New("authorized sender has no signing profile")
}

// ValidateProfilesFile checks syntax and keys without performing network I/O.
func ValidateProfilesFile(path, authserv string) error {
	_, err := loadProfiles(context.TODO(), path, authserv, nil)
	return err
}

func loadProfiles(ctx context.Context, path, authserv string, resolver TXTResolver) (*profileSnapshot, error) {
	fail := func() (*profileSnapshot, error) {
		return nil, errors.New("invalid or unavailable DKIM signing profiles")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return fail()
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	file.Close()
	if err != nil || len(data) > 1<<20 {
		return fail()
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest profileManifest
	if err := decoder.Decode(&manifest); err != nil {
		return fail()
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fail()
	}
	if manifest.SchemaVersion != 1 || strings.TrimSpace(manifest.Revision) == "" || len(manifest.Revision) > 128 || strings.ContainsAny(manifest.Revision, "\r\n\x00") {
		return fail()
	}
	if manifest.UnknownDomain == "" {
		manifest.UnknownDomain = "reject"
	}
	if manifest.UnknownDomain != "reject" && manifest.UnknownDomain != "unsigned" {
		return fail()
	}
	if len(manifest.Profiles) == 0 || len(manifest.Profiles) > 64 {
		return fail()
	}
	snapshot := &profileSnapshot{revision: manifest.Revision, unsignedUnknown: manifest.UnknownDomain == "unsigned", signers: map[string]ProfileSigners{}}
	hash := sha256.New()
	hash.Write(data)
	for _, p := range manifest.Profiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.Domain = strings.ToLower(p.Domain)
		p.Selector = strings.ToLower(p.Selector)
		if !validSigningName(p.Domain) || !validSigningName(p.Selector) || len(p.Selector+"._domainkey."+p.Domain) > 253 || p.KeyFile == "" {
			return fail()
		}
		if _, exists := snapshot.signers[p.Domain]; exists {
			return fail()
		}
		headers := parseHeaderList(p.Headers)
		if len(headers) == 0 {
			headers = []string{"from", "to", "subject", "date", "message-id"}
		}
		hasFrom := false
		for _, header := range headers {
			if header == "from" {
				hasFrom = true
			}
			for _, ch := range header {
				if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
					return fail()
				}
			}
		}
		if !hasFrom {
			return fail()
		}
		keyPath := p.KeyFile
		if !filepath.IsAbs(keyPath) {
			keyPath = filepath.Join(filepath.Dir(path), keyPath)
		}
		key, err := loadPrivateKey(keyPath)
		if err != nil {
			return fail()
		}
		if p.Algorithm != signatureAlgorithm(key) {
			return fail()
		}
		public, err := x509.MarshalPKIXPublicKey(key.Public())
		if err != nil {
			return fail()
		}
		hash.Write(public)
		if resolver != nil {
			records, err := resolver.LookupTXT(ctx, p.Selector+"._domainkey."+p.Domain)
			if err != nil || !publishedKeyMatches(records, key.Public(), p.Algorithm) {
				return fail()
			}
		}
		signer := &FileSigner{domain: p.Domain, selector: p.Selector, headers: headers, privKey: key, fixed: true}
		pair := ProfileSigners{DKIM: signer}
		if p.ARC {
			a := authserv
			if a == "" {
				a = "kuroshio.local"
			}
			pair.ARC = &ARCFileSigner{domain: p.Domain, selector: p.Selector, headers: headers, authserv: a, privateKey: key, fixed: true}
		}
		snapshot.signers[p.Domain] = pair
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	copy(snapshot.digest[:], hash.Sum(nil))
	return snapshot, nil
}

func validSigningName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}
