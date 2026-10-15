package delivery

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestPrepareOutboundDataValidatesARCChain(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(config.Config{
		Hostname:           "mx.example.com",
		DKIMSignDomain:     "example.com",
		DKIMSignSelector:   "s1",
		DKIMPrivateKeyFile: keyPath,
		DKIMSignHeaders:    "from:subject",
	})
	if client.signer == nil || client.arcSigner == nil {
		t.Fatal("signers were not initialized")
	}
	tests := []struct {
		name    string
		headers string
		prefix  string
	}{
		{name: "new message", prefix: "ARC-Seal: i=1; cv=none;"},
		{
			name: "untrusted result cannot validate incomplete chain",
			headers: "ARC-Seal: i=1; cv=none; b=invalid\r\n" +
				"Authentication-Results: mx.example.com; arc=pass\r\n",
			prefix: "ARC-Seal: i=2; cv=fail;",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(tt.headers + "From: a@example.com\r\nSubject: hello\r\n\r\nbody\r\n")
			got, err := client.prepareOutboundData(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(got), tt.prefix) {
				t.Fatalf("missing expected seal: %s", got)
			}
			if !strings.Contains(string(got), "\r\nDKIM-Signature: ") {
				t.Fatal("DKIM must be applied before ARC")
			}
			if !strings.HasSuffix(string(got), string(raw)) {
				t.Fatal("original message modified")
			}
		})
	}
}
