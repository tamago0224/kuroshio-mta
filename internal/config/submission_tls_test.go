package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubmissionTLSValidation(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantError bool
	}{
		{name: "disabled"},
		{name: "password TLS required", cfg: Config{SubmissionAddr: ":587"}, wantError: true},
		{name: "isolated plaintext", cfg: Config{SubmissionAddr: "127.0.0.1:587", SubmissionAllowInsecureAuth: true}},
		{name: "implicit requires cert even with exception", cfg: Config{SubmissionTLSAddr: ":465", SubmissionAllowInsecureAuth: true}, wantError: true},
		{name: "partial key pair", cfg: Config{TLSCertFile: "missing"}, wantError: true},
		{name: "bad certificate", cfg: Config{SubmissionTLSAddr: ":465", TLSCertFile: "missing", TLSKeyFile: "missing"}, wantError: true},
		{name: "conflicting listeners", cfg: Config{SubmissionTLSAddr: ":465", SubmissionAddr: ":465"}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.cfg.validateSubmissionTLS(); (err != nil) != tt.wantError {
				t.Fatalf("error=%v wantError=%v", err, tt.wantError)
			}
		})
	}
}

func TestSubmissionTLSConfigSources(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("MTA_CONFIG_FILE", "")
	t.Setenv("MTA_SUBMISSION_ADDR", "")
	t.Setenv("MTA_SUBMISSION_TLS_ADDR", "")
	t.Setenv("MTA_SUBMISSION_ALLOW_INSECURE_AUTH", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SubmissionAllowInsecureAuth || cfg.SubmissionTLSAddr != "" {
		t.Fatal("unsafe defaults")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("submission_addr: '127.0.0.1:587'\nsubmission_allow_insecure_auth: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SubmissionAllowInsecureAuth {
		t.Fatal("YAML exception not loaded")
	}
	t.Setenv("MTA_SUBMISSION_ALLOW_INSECURE_AUTH", "false")
	if _, err = LoadWithPath(path); err == nil {
		t.Fatal("environment must disable YAML exception")
	}
	t.Setenv("MTA_SUBMISSION_ALLOW_INSECURE_AUTH", "true")
	t.Setenv("MTA_SUBMISSION_TLS_ADDR", ":465")
	if _, err = LoadWithPath(path); err == nil {
		t.Fatal("implicit TLS environment setting must require certificate")
	}
}
