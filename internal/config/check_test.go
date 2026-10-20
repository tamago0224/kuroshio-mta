package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConfigRejectsInvalidInputsWithoutValues(t *testing.T) {
	const secret = "sensitive-canary-value"
	tests := []struct{ name, body string }{
		{"unknown field", "passwrod: " + secret},
		{"nested unknown", "smtp_limits:\n  max_recepients: 1"},
		{"bad integer", "worker_count: " + secret},
		{"negative count", "worker_count: -1"},
		{"zero workers", "worker_count: 0"},
		{"invalid enum", "rate_limit_backend: " + secret},
		{"invalid delivery", "delivery_mode: " + secret},
		{"invalid duration", "send_timeout: " + secret},
		{"multiple documents", "hostname: one\n---\nhostname: two"},
		{"missing TLS key", "tls_cert_file: " + secret},
		{"bad TLS pair", "tls_cert_file: missing-cert\ntls_key_file: " + secret},
		{"missing signing profile", "dkim_profiles_file: " + secret},
		{"incomplete DKIM", "dkim_sign_domain: example.test"},
		{"bad DKIM key", "dkim_sign_domain: example.test\ndkim_sign_selector: s1\ndkim_private_key_file: " + secret},
		{"missing relay", "delivery_mode: relay"},
		{"missing route credential", "delivery_routes:\n  - name: auth\n    domains: [example.test]\n    mode: relay\n    relay_host: relay.example.test\n    relay_require_tls: true\n    relay_username: sender\n    relay_password_file: " + secret},
		{"bad rule", "rate_limit_rules: " + secret},
		{"capacity backend", "queue_backend: kafka\nqueue_max_messages: 10"},
		{"bad sample ratio", "otel_trace_sample_ratio: 3"},
		{"bad floating value", "reputation_bounce_threshold: .nan"},
		{"duplicate address", "listen_addr: ':2525'\nadmin_addr: ':2525'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.body+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			err := CheckWithPath(path)
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("input value leaked")
			}
		})
	}
}

func TestCheckConfigEnvironmentAndCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("unknown_legacy_key: ignored\nrate_limit_backend: invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithPath(path); err != nil {
		t.Fatalf("legacy load changed: %v", err)
	}
	if err := os.WriteFile(path, []byte("hostname: mx.example.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckWithPath(path); err != nil {
		t.Fatalf("valid baseline: %v", err)
	}
	for _, tt := range []struct{ name, key, value string }{
		{"integer", "MTA_WORKER_COUNT", "bad"},
		{"boolean", "MTA_SUBMISSION_AUTH_REQUIRED", "bad"},
		{"enum", "MTA_RATE_LIMIT_BACKEND", "bad"},
		{"duration", "MTA_SCAN_INTERVAL", "bad"},
		{"durations", "MTA_RETRY_SCHEDULE", "1s,bad"},
		{"float", "MTA_OTEL_TRACE_SAMPLE_RATIO", "NaN"},
		{"range", "MTA_OTEL_TRACE_SAMPLE_RATIO", "2"},
		{"secret file", "MTA_ADMIN_TOKENS_FILE", filepath.Join(t.TempDir(), "missing")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			err := CheckWithPath(path)
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("expected diagnostic for %s, got %v", tt.key, err)
			}
		})
	}
}

func TestCheckConfigLeavesQueueAndDatabaseUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	queue := filepath.Join(dir, "queue")
	database := filepath.Join(dir, "users.sqlite")
	body := "queue_dir: " + queue + "\nsubmission_addr: '127.0.0.1:1587'\nsubmission_allow_insecure_auth: true\nsubmission_auth_backend: sqlite\nsubmission_auth_dsn: " + database + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := CheckWithPath(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []string{queue, database} {
		if _, err := os.Stat(resource); !os.IsNotExist(err) {
			t.Fatalf("resource created: %s (%v)", resource, err)
		}
	}
	if err := os.WriteFile(database, []byte("existing database canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckWithPath(path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(database)
	if err != nil || string(contents) != "existing database canary" {
		t.Fatal("existing DB changed")
	}
}
