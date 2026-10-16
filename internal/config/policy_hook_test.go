package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPolicyHookConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("policy_hook:\n  url: https://filter.example.test/check\n  timeout: 2s\n  max_concurrent: 4\n  max_response_bytes: 2048\n  send_body: true\n  token_file: /yaml/token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MTA_POLICY_HOOK_TOKEN_FILE", "/environment/token")
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PolicyHook.Timeout != 2*time.Second || !cfg.PolicyHook.SendBody || cfg.PolicyHook.TokenFile != "/environment/token" || cfg.PolicyHook.MaxConcurrent != 4 {
		t.Fatal("hook configuration mismatch")
	}
	for _, tc := range []struct {
		name string
		cfg  PolicyHookConfig
	}{
		{"external HTTP", PolicyHookConfig{URL: "http://filter.example.test"}},
		{"loopback without optin", PolicyHookConfig{URL: "http://127.0.0.1"}},
		{"hostname rebinding", PolicyHookConfig{URL: "http://localhost", AllowHTTPLoopback: true}},
		{"userinfo secret", PolicyHookConfig{URL: "https://user:secret@filter.example.test"}},
		{"query secret", PolicyHookConfig{URL: "https://filter.example.test?token=secret"}},
		{"fragment", PolicyHookConfig{URL: "https://filter.example.test#fragment"}},
		{"negative timeout", PolicyHookConfig{URL: "https://filter.example.test", Timeout: -time.Second}},
		{"oversized limit", PolicyHookConfig{URL: "https://filter.example.test", MaxResponseBytes: 2 * 1024 * 1024}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("unsafe hook configuration accepted")
			}
		})
	}
}
