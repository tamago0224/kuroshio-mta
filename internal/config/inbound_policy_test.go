package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInboundPolicyConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("inbound_policy:\n  local_domains: [local.test]\n  local_recipients: [user@local.test]\n  trusted_cidrs: ['192.0.2.0/24', '2001:db8::/32']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.InboundPolicy.TrustedCIDRs) != 2 || len(cfg.InboundPolicy.LocalRecipients) != 1 || len(cfg.InboundPolicy.LocalDomains) != 1 {
		t.Fatal("YAML policy not loaded")
	}
}

func TestInboundPolicyValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       InboundPolicyConfig
		wantError bool
	}{
		{name: "empty denies everything"},
		{name: "valid", cfg: InboundPolicyConfig{LocalDomains: []string{"LOCAL.test"}, LocalRecipients: []string{"User@local.test"}, TrustedCIDRs: []string{"192.0.2.0/24", "2001:db8::/32"}}},
		{name: "bad CIDR", cfg: InboundPolicyConfig{TrustedCIDRs: []string{"192.0.2.1"}}, wantError: true},
		{name: "wildcard domain", cfg: InboundPolicyConfig{LocalDomains: []string{"*.local.test"}}, wantError: true},
		{name: "bad domain", cfg: InboundPolicyConfig{LocalDomains: []string{"local..test"}}, wantError: true},
		{name: "external mailbox", cfg: InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"user@external.test"}}, wantError: true},
		{name: "display name", cfg: InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"User <user@local.test>"}}, wantError: true},
		{name: "invalid mailbox", cfg: InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"bad@@local.test"}}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.cfg.Validate(); (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestInboundPolicyExamples(t *testing.T) {
	paths, err := filepath.Glob("../../examples/tutorials/*/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, "../../config.example.yaml", "../../config.local.example.yaml")
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			cfg, err := loadYAMLConfig(path, defaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.InboundPolicy.Validate(); err != nil {
				t.Fatal(err)
			}
			if len(cfg.InboundPolicy.LocalRecipients) == 0 && len(cfg.InboundPolicy.TrustedCIDRs) == 0 {
				t.Fatal("example must declare intended recipient admission")
			}
		})
	}
}
