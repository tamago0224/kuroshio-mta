package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSigningProfilePathAndLegacyExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("dkim_profiles_file: /yaml/profiles.json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MTA_DKIM_PROFILES_FILE", "/environment/profiles.json")
	cfg, err := LoadWithPath(path)
	if err != nil || cfg.DKIMProfilesFile != "/environment/profiles.json" {
		t.Fatalf("profile config: %+v %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte("dkim_profiles_file: /yaml/profiles.json\ndkim_sign_domain: sender.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithPath(path); err == nil {
		t.Fatal("legacy signing and profiles were combined")
	}
}
