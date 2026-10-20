package delivery

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

type signingProfileDNS struct{ record string }

func (r signingProfileDNS) LookupTXT(context.Context, string) ([]string, error) {
	return []string{r.record}, nil
}
func profileClientForTest(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "private.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{"schema_version": 1, "revision": "one", "unknown_domain": "reject", "profiles": []dkim.Profile{{Domain: "sender.test", Selector: "s1", Algorithm: "ed25519-sha256", KeyFile: keyPath, Headers: "from:subject"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(path, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := dkim.NewProfileManager(t.Context(), path, "mx.test", signingProfileDNS{record: "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(public)})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(config.Config{DeliveryMode: "local_spool", LocalSpoolDir: filepath.Join(dir, "spool")})
	client.profiles = manager
	t.Cleanup(func() { client.Close() })
	return client
}
func TestProfileSigningRequiresTrustedPersistedIdentity(t *testing.T) {
	t.Parallel()
	client := profileClientForTest(t)
	for _, tc := range []struct {
		name         string
		identity     *model.SigningIdentity
		from         string
		fail, signed bool
	}{
		{name: "authorized", identity: &model.SigningIdentity{Domain: "sender.test"}, from: "sender.test", signed: true},
		{name: "untrusted inbound", identity: &model.SigningIdentity{}, from: "sender.test"},
		{name: "legacy unknown", from: "sender.test", fail: true},
		{name: "persisted mismatch", identity: &model.SigningIdentity{Domain: "other.test"}, from: "sender.test", fail: true},
		{name: "unknown profile", identity: &model.SigningIdentity{Domain: "other.test"}, from: "other.test", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := &model.Message{ID: tc.name, MailFrom: "user@" + tc.from, SigningIdentity: tc.identity, Data: []byte("From: user@" + tc.from + "\r\nSubject: test\r\n\r\nbody")}
			got, err := client.prepareOutboundMessage(t.Context(), msg)
			if (err != nil) != tc.fail {
				t.Fatalf("prepare: %v", err)
			}
			if !tc.fail && strings.HasPrefix(string(got), "DKIM-Signature:") != tc.signed {
				t.Fatal("incorrect signing decision")
			}
		})
	}
}
func TestProfileMetadataReachesLocalAndS3Spool(t *testing.T) {
	t.Parallel()
	client := profileClientForTest(t)
	msg := &model.Message{ID: "signed", MailFrom: "user@sender.test", SigningIdentity: &model.SigningIdentity{Domain: "sender.test"}, Data: []byte("From: user@sender.test\r\nSubject: test\r\n\r\nbody")}
	if err := client.Deliver(t.Context(), msg, "user@recipient.test"); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(client.cfg.LocalSpoolDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("spool: %v %v", files, err)
	}
	data, err := os.ReadFile(filepath.Join(client.cfg.LocalSpoolDir, files[0].Name()))
	if err != nil || !strings.Contains(string(data), "d=sender.test; s=s1;") {
		t.Fatalf("local signing: %v", err)
	}
	fake := &fakeS3Client{}
	store := &s3SpoolStore{cfg: config.Config{SpoolS3Bucket: "test"}, client: fake, prepareMessage: client.prepareOutboundMessage}
	if _, err := store.Store(t.Context(), msg, "user@recipient.test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fake.body), "d=sender.test; s=s1;") {
		t.Fatal("S3 lost trusted identity")
	}
}

func TestProfileRawPreparationCannotBypassIdentity(t *testing.T) {
	t.Parallel()
	client := profileClientForTest(t)
	if _, err := client.prepareOutboundData([]byte("From: user@sender.test\r\n\r\nbody")); err == nil {
		t.Fatal("raw-only entry point bypassed signing identity")
	}
	data := []byte("From: MAILER-DAEMON@mx.test\r\nSubject: failure\r\n\r\nreport")
	if got, err := client.prepareOutboundMessage(t.Context(), &model.Message{Data: data}); err != nil || string(got) != string(data) {
		t.Fatalf("automatic null-sender report policy: %v", err)
	}
}
