package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProxyProtocolConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("proxy_protocol:\n  smtp: true\n  submission: true\n  submission_tls: true\n  trusted_cidrs: ['192.0.2.0/24', '2001:db8::/32']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ProxyProtocol.SMTP || !cfg.ProxyProtocol.Submission || !cfg.ProxyProtocol.SubmissionTLS || len(cfg.ProxyProtocol.TrustedCIDRs) != 2 {
		t.Fatal("proxy YAML not loaded")
	}
	for _, tt := range []struct {
		name    string
		cfg     ProxyProtocolConfig
		invalid bool
	}{
		{name: "disabled"},
		{name: "missing trust", cfg: ProxyProtocolConfig{SMTP: true}, invalid: true},
		{name: "bad CIDR", cfg: ProxyProtocolConfig{TrustedCIDRs: []string{"127.0.0.1"}}, invalid: true},
		{name: "IPv6", cfg: ProxyProtocolConfig{SubmissionTLS: true, TrustedCIDRs: []string{"::1/128"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.invalid {
				t.Fatalf("Validate=%v", err)
			}
		})
	}
}
