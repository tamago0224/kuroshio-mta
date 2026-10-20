package dkim

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type profileDNS struct {
	mu      sync.RWMutex
	records map[string][]string
}

func (r *profileDNS) LookupTXT(ctx context.Context, name string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	records, ok := r.records[name]
	if !ok {
		return nil, errors.New("not published")
	}
	return append([]string{}, records...), nil
}
func (r *profileDNS) put(name, record string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[name] = []string{record}
}

func profileTestKey(t *testing.T, dir, name, algorithm string) (string, crypto.PublicKey, string) {
	t.Helper()
	var key crypto.Signer
	var err error
	if algorithm == "rsa-sha256" {
		key, err = rsa.GenerateKey(rand.Reader, 1024)
	} else {
		_, private, genErr := ed25519.GenerateKey(rand.Reader)
		key = private
		err = genErr
	}
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	var data []byte
	kind := "ed25519"
	if public, ok := key.Public().(ed25519.PublicKey); ok {
		data = public
	} else {
		kind = "rsa"
		data, err = x509.MarshalPKIXPublicKey(key.Public())
		if err != nil {
			t.Fatal(err)
		}
	}
	return path, key.Public(), "v=DKIM1; k=" + kind + "; p=" + base64.StdEncoding.EncodeToString(data)
}
func writeProfileManifest(t *testing.T, path, revision, unknown string, profiles ...Profile) {
	t.Helper()
	data, err := json.Marshal(profileManifest{SchemaVersion: 1, Revision: revision, UnknownDomain: unknown, Profiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func verifyProfileSignature(t *testing.T, raw []byte, public crypto.PublicKey) {
	t.Helper()
	header, _, ok := splitMessage(raw)
	if !ok {
		t.Fatal("invalid signed message")
	}
	first, _, _ := strings.Cut(header, "\r\n")
	value := strings.TrimPrefix(first, "DKIM-Signature: ")
	unsigned, encoded, ok := strings.Cut(value, "; b=")
	if !ok {
		t.Fatal("missing signature")
	}
	unsigned += "; b="
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture signs exactly From and Subject using relaxed canonicalization.
	parsed := parseRawHeaders(header)
	_, canonical := buildSignedHeaders(parsed, []string{"from", "subject"})
	digest := sha256.Sum256([]byte(canonical + strings.TrimSuffix(canonHeaderRelaxed("DKIM-Signature", unsigned), "\r\n")))
	switch key := public.(type) {
	case ed25519.PublicKey:
		if !ed25519.Verify(key, digest[:], signature) {
			t.Fatal("signature does not match selected Ed25519 key")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
			t.Fatal("signature does not match selected RSA key")
		}
	default:
		t.Fatal("unexpected public key")
	}
}

func TestProfilesAtomicRotationKeepsInFlightKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := filepath.Join(dir, "profiles.json")
	dns := &profileDNS{records: map[string][]string{}}
	oldPath, oldPublic, oldTXT := profileTestKey(t, dir, "old", "ed25519-sha256")
	newPath, newPublic, newTXT := profileTestKey(t, dir, "new", "rsa-sha256")
	profile := Profile{Domain: "sender.test", Selector: "old", Algorithm: "ed25519-sha256", KeyFile: oldPath, Headers: "from:subject", ARC: true}
	writeProfileManifest(t, manifest, "one", "reject", profile)
	if _, err := NewProfileManager(t.Context(), manifest, "mx.test", dns); err == nil {
		t.Fatal("unpublished key activated")
	}
	dns.put("old._domainkey.sender.test", oldTXT)
	manager, err := NewProfileManager(t.Context(), manifest, "mx.test", dns)
	if err != nil {
		t.Fatal(err)
	}
	inFlight, err := manager.Signers("sender.test")
	if err != nil {
		t.Fatal(err)
	}
	profile.Selector = "new"
	profile.Algorithm = "rsa-sha256"
	profile.KeyFile = newPath
	writeProfileManifest(t, manifest, "two", "reject", profile)
	if _, err := manager.Reload(t.Context()); err == nil {
		t.Fatal("unpublished switch accepted")
	}
	stillOld, _ := manager.Signers("sender.test")
	raw := []byte("From: user@sender.test\r\nSubject: test\r\n\r\nbody\r\n")
	got, err := stillOld.DKIM.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyProfileSignature(t, got, oldPublic)
	dns.put("new._domainkey.sender.test", newTXT)
	if changed, err := manager.Reload(t.Context()); err != nil || !changed {
		t.Fatalf("activate: %v %v", changed, err)
	}
	if err := os.Rename(oldPath, oldPath+".retained"); err != nil {
		t.Fatal(err)
	}
	got, err = inFlight.DKIM.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyProfileSignature(t, got, oldPublic)
	current, _ := manager.Signers("sender.test")
	got, err = current.DKIM.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyProfileSignature(t, got, newPublic)
	if current.ARC == nil {
		t.Fatal("ARC profile lost")
	}
	sealed, err := current.ARC.SignWithChainValidation(got, "none")
	if err != nil || !strings.Contains(string(sealed), "a=rsa-sha256;") {
		t.Fatalf("ARC snapshot mismatch: %v", err)
	}
	replacement, _, replacementTXT := profileTestKey(t, dir, "same-selector-replacement", "rsa-sha256")
	profile.KeyFile = replacement
	writeProfileManifest(t, manifest, "three", "reject", profile)
	dns.put("new._domainkey.sender.test", replacementTXT)
	if _, err := manager.Reload(t.Context()); err == nil {
		t.Fatal("active selector key changed without a new selector")
	}
	if err := os.Rename(oldPath+".retained", oldPath); err != nil {
		t.Fatal(err)
	}
	profile.Selector = "old"
	profile.Algorithm = "ed25519-sha256"
	profile.KeyFile = oldPath
	writeProfileManifest(t, manifest, "rollback", "reject", profile)
	if changed, err := manager.Reload(t.Context()); err != nil || !changed {
		t.Fatalf("rollback: %v %v", changed, err)
	}
	rolledBack, _ := manager.Signers("sender.test")
	got, err = rolledBack.DKIM.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyProfileSignature(t, got, oldPublic)
	if _, err := manager.Signers("unknown.test"); err == nil {
		t.Fatal("unknown domain silently unsigned")
	}
}

func TestProfilesMultipleDomainsAndConcurrentReaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	dns := &profileDNS{records: map[string][]string{}}
	first, _, firstTXT := profileTestKey(t, dir, "a", "ed25519-sha256")
	second, _, secondTXT := profileTestKey(t, dir, "b", "ed25519-sha256")
	profiles := []Profile{{Domain: "a.test", Selector: "s1", Algorithm: "ed25519-sha256", KeyFile: first, Headers: "from:subject"}, {Domain: "b.test", Selector: "s2", Algorithm: "ed25519-sha256", KeyFile: second, Headers: "from:subject"}}
	writeProfileManifest(t, path, "one", "unsigned", profiles...)
	dns.put("s1._domainkey.a.test", firstTXT)
	dns.put("s2._domainkey.b.test", secondTXT)
	dns.put("s3._domainkey.b.test", secondTXT)
	manager, err := NewProfileManager(t.Context(), path, "mx.test", dns)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				pair, err := manager.Signers("b.test")
				if err != nil {
					t.Error(err)
					return
				}
				got, err := pair.DKIM.Sign([]byte("From: user@b.test\r\nSubject: test\r\n\r\nbody"))
				if err != nil || !(strings.Contains(string(got), "d=b.test; s=s2;") || strings.Contains(string(got), "d=b.test; s=s3;")) {
					t.Error("domain profile mismatch")
				}
			}
		})
	}
	for i := range 10 {
		profiles[1].Selector = "s2"
		if i%2 == 0 {
			profiles[1].Selector = "s3"
		}
		writeProfileManifest(t, path, strconv.Itoa(i), "unsigned", profiles...)
		if _, err := manager.Reload(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if pair, err := manager.Signers("unknown.test"); err != nil || pair.DKIM != nil {
		t.Fatal("unsigned unknown policy ignored")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := manager.Reload(canceled); err == nil {
		t.Fatal("canceled reload accepted")
	}
}

func TestProfilesRejectKeyMismatchAndMalformedManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	dns := &profileDNS{records: map[string][]string{}}
	key, _, txt := profileTestKey(t, dir, "one", "ed25519-sha256")
	_, _, other := profileTestKey(t, dir, "other", "ed25519-sha256")
	p := Profile{Domain: "sender.test", Selector: "s1", Algorithm: "ed25519-sha256", KeyFile: key}
	writeProfileManifest(t, path, "one", "reject", p)
	dns.put("s1._domainkey.sender.test", other)
	if _, err := NewProfileManager(t.Context(), path, "mx.test", dns); err == nil {
		t.Fatal("different public key accepted")
	}
	dns.put("s1._domainkey.sender.test", txt)
	p.Algorithm = "rsa-sha256"
	writeProfileManifest(t, path, "two", "reject", p)
	if err := ValidateProfilesFile(path, "mx.test"); err == nil {
		t.Fatal("algorithm mismatch accepted")
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"secret":"PRIVATE-KEY-SENTINEL"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfilesFile(path, "mx.test"); err == nil || strings.Contains(err.Error(), "PRIVATE-KEY-SENTINEL") || strings.Contains(err.Error(), dir) {
		t.Fatal("invalid manifest accepted or leaked input")
	}
}

func TestPublishedKeyActivationConstraints(t *testing.T) {
	t.Parallel()
	_, public, record := profileTestKey(t, t.TempDir(), "ed", "ed25519-sha256")
	for _, tc := range []struct {
		name    string
		records []string
		valid   bool
	}{
		{name: "valid", records: []string{record}, valid: true},
		{name: "multiple records", records: []string{record, record}},
		{name: "empty version", records: []string{strings.Replace(record, "v=DKIM1", "v=", 1)}},
		{name: "wrong algorithm", records: []string{strings.Replace(record, "k=ed25519", "k=rsa", 1)}},
		{name: "revoked", records: []string{"v=DKIM1; k=ed25519; p="}},
		{name: "SHA1 only", records: []string{record + "; h=sha1"}},
		{name: "wrong service", records: []string{record + "; s=http"}},
		{name: "duplicate p", records: []string{record + "; p=duplicate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publishedKeyMatches(tc.records, public, "ed25519-sha256"); got != tc.valid {
				t.Fatalf("activation check: %v", got)
			}
		})
	}
}

type cancelingProfileDNS struct {
	inner  TXTResolver
	cancel context.CancelFunc
}

func (r cancelingProfileDNS) LookupTXT(ctx context.Context, name string) ([]string, error) {
	records, err := r.inner.LookupTXT(ctx, name)
	r.cancel()
	return records, err
}
func TestCanceledProfileActivationKeepsPriorSet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	dns := &profileDNS{records: map[string][]string{}}
	key, _, record := profileTestKey(t, dir, "one", "ed25519-sha256")
	profile := Profile{Domain: "sender.test", Selector: "s1", Algorithm: "ed25519-sha256", KeyFile: key, Headers: "from:subject"}
	writeProfileManifest(t, path, "one", "reject", profile)
	dns.put("s1._domainkey.sender.test", record)
	dns.put("s2._domainkey.sender.test", record)
	manager, err := NewProfileManager(t.Context(), path, "mx.test", dns)
	if err != nil {
		t.Fatal(err)
	}
	profile.Selector = "s2"
	writeProfileManifest(t, path, "two", "reject", profile)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	manager.resolver = cancelingProfileDNS{inner: dns, cancel: cancel}
	if _, err := manager.Reload(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled activation: %v", err)
	}
	pair, err := manager.Signers("sender.test")
	if err != nil {
		t.Fatal(err)
	}
	data, err := pair.DKIM.Sign([]byte("From: user@sender.test\r\nSubject: test\r\n\r\nbody"))
	if err != nil || !strings.Contains(string(data), "s=s1;") {
		t.Fatal("cancellation published candidate profile")
	}
}
