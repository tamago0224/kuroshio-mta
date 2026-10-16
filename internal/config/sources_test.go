package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfigFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadYAMLZeroValuesAndEnvFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{
		"MTA_CONFIG_FILE", "MTA_HOSTNAME", "MTA_SUBMISSION_AUTH_REQUIRED",
		"MTA_WORKER_COUNT", "MTA_DNSBL_CACHE_TTL", "MTA_SPOOL_BACKEND",
	} {
		t.Setenv(key, "")
	}
	writeConfigFixture(t, "input.yaml", "hostname: ''\nsubmission_auth_required: false\nworker_count: 0\ndnsbl_cache_ttl: 0s\nspool_backend: s3\n")
	cfg := mustLoadWithPathForTest(t, "input.yaml")
	if cfg.Hostname != "" || cfg.SubmissionAuth || cfg.WorkerCount != 0 || cfg.DNSBLCacheTTL != 0 {
		t.Fatal("explicit YAML empty, false or zero values were lost")
	}
	if cfg.ListenAddr != ":2525" {
		t.Fatal("omitted YAML field did not retain its default")
	}
	for _, key := range []string{"MTA_SUBMISSION_AUTH_REQUIRED", "MTA_WORKER_COUNT", "MTA_DNSBL_CACHE_TTL", "MTA_SPOOL_BACKEND"} {
		t.Setenv(key, "invalid")
	}
	cfg = mustLoadWithPathForTest(t, "input.yaml")
	if cfg.SubmissionAuth || cfg.WorkerCount != 0 || cfg.DNSBLCacheTTL != 0 || cfg.SpoolBackend != "s3" {
		t.Fatal("invalid environment values did not fall back to YAML")
	}
	t.Setenv("MTA_SUBMISSION_AUTH_REQUIRED", "true")
	t.Setenv("MTA_WORKER_COUNT", "2")
	t.Setenv("MTA_DNSBL_CACHE_TTL", "1m")
	cfg = mustLoadWithPathForTest(t, "input.yaml")
	if !cfg.SubmissionAuth || cfg.WorkerCount != 2 || cfg.DNSBLCacheTTL != time.Minute {
		t.Fatal("valid environment values did not override YAML")
	}
}

func TestLoadDefaultFileSelection(t *testing.T) {
	tests := []struct {
		name string
		yaml bool
		yml  bool
		env  string
		want string
		fail bool
	}{
		{name: "defaults", want: "kuroshio.local"},
		{name: "yml", yml: true, want: "yml.example"},
		{name: "yaml before yml", yaml: true, yml: true, want: "yaml.example"},
		{name: "environment before default files", yaml: true, yml: true, env: "env.yaml", want: "env.example"},
		{name: "missing environment path does not fall back", yaml: true, env: "missing.yaml", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("MTA_CONFIG_FILE", tt.env)
			t.Setenv("MTA_HOSTNAME", "")
			if tt.yaml {
				writeConfigFixture(t, "config.yaml", "hostname: yaml.example\n")
			}
			if tt.yml {
				writeConfigFixture(t, "config.yml", "hostname: yml.example\n")
			}
			writeConfigFixture(t, "env.yaml", "hostname: env.example\n")
			cfg, err := Load()
			if tt.fail {
				if err == nil {
					t.Fatal("expected missing explicit file error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Hostname != tt.want {
				t.Fatalf("hostname = %q, want %q", cfg.Hostname, tt.want)
			}
		})
	}
}

func TestLoadSecretFileFallback(t *testing.T) {
	for _, secret := range []struct {
		name  string
		field string
		key   string
		get   func(Config) string
	}{
		{name: "submission users", field: "submission_users", key: "MTA_SUBMISSION_USERS", get: func(c Config) string { return c.SubmissionUsers }},
		{name: "admin tokens", field: "admin_tokens", key: "MTA_ADMIN_TOKENS", get: func(c Config) string { return c.AdminTokens }},
		{name: "bearer client secret", field: "submission_bearer_client_secret", key: "MTA_SUBMISSION_BEARER_CLIENT_SECRET", get: func(c Config) string { return c.SubmissionBearerClientSecret }},
	} {
		t.Run(secret.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "input.yaml")
			writeConfigFixture(t, configPath, secret.field+": yaml-value\n")
			for _, tt := range []struct {
				name    string
				content string
				missing bool
				env     string
				want    string
			}{
				{name: "file wins", content: " file-value\n", env: "env-value", want: "file-value"},
				{name: "empty file uses environment", content: " \n", env: "env-value", want: "env-value"},
				{name: "missing file uses environment", missing: true, env: "env-value", want: "env-value"},
				{name: "missing file and empty environment use YAML", missing: true, want: "yaml-value"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "secret")
					if !tt.missing {
						writeConfigFixture(t, path, tt.content)
					}
					t.Setenv(secret.key, tt.env)
					t.Setenv(secret.key+"_FILE", path)
					cfg := mustLoadWithPathForTest(t, configPath)
					if secret.get(cfg) != tt.want {
						t.Fatal("secret source precedence changed")
					}
				})
			}
		})
	}
}
