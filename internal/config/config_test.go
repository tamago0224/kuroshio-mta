package config

import (
	"os"
	"testing"
	"time"
)

func TestEnvDurationList(t *testing.T) {
	def := []time.Duration{time.Minute, 2 * time.Minute}

	t.Setenv("MTA_RETRY_SCHEDULE", "5m,30m,2h")
	got := envDurationList("MTA_RETRY_SCHEDULE", def)
	want := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d len(want)=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%s want=%s", i, got[i], want[i])
		}
	}

	t.Setenv("MTA_RETRY_SCHEDULE", "bad")
	got = envDurationList("MTA_RETRY_SCHEDULE", def)
	if len(got) != len(def) || got[0] != def[0] || got[1] != def[1] {
		t.Fatalf("invalid value should fall back to default: got=%v def=%v", got, def)
	}

	_ = os.Unsetenv("MTA_RETRY_SCHEDULE")
}

func TestEnvCSV(t *testing.T) {
	def := []string{"a.example.org"}
	t.Setenv("MTA_DNSBL_ZONES", "zen.example.org, bl.example.net")
	got := envCSV("MTA_DNSBL_ZONES", def)
	if len(got) != 2 || got[0] != "zen.example.org" || got[1] != "bl.example.net" {
		t.Fatalf("unexpected csv parse: %v", got)
	}

	t.Setenv("MTA_DNSBL_ZONES", " , ")
	got = envCSV("MTA_DNSBL_ZONES", def)
	if len(got) != 1 || got[0] != def[0] {
		t.Fatalf("expected fallback default, got=%v", got)
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("MTA_RELAY_REQUIRE_TLS", "true")
	if !envBool("MTA_RELAY_REQUIRE_TLS", false) {
		t.Fatal("true should parse as true")
	}
	t.Setenv("MTA_RELAY_REQUIRE_TLS", "0")
	if envBool("MTA_RELAY_REQUIRE_TLS", true) {
		t.Fatal("0 should parse as false")
	}
	t.Setenv("MTA_RELAY_REQUIRE_TLS", "invalid")
	if !envBool("MTA_RELAY_REQUIRE_TLS", true) {
		t.Fatal("invalid value should fallback to default")
	}
}

func TestLoadRateLimitRules(t *testing.T) {
	t.Setenv("MTA_RATE_LIMIT_RULES", "connect:ip:10:1m")
	cfg := Load()
	if cfg.RateLimitRules != "connect:ip:10:1m" {
		t.Fatalf("unexpected rules: %q", cfg.RateLimitRules)
	}
}

func TestLoadSubmissionConfig(t *testing.T) {
	t.Setenv("MTA_SUBMISSION_ADDR", ":587")
	t.Setenv("MTA_SUBMISSION_AUTH_REQUIRED", "true")
	t.Setenv("MTA_SUBMISSION_USERS", "alice@example.com:s3cr3t")
	t.Setenv("MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY", "true")

	cfg := Load()
	if cfg.SubmissionAddr != ":587" {
		t.Fatalf("submission addr=%q", cfg.SubmissionAddr)
	}
	if !cfg.SubmissionAuth {
		t.Fatal("submission auth should be true")
	}
	if cfg.SubmissionUsers != "alice@example.com:s3cr3t" {
		t.Fatalf("submission users=%q", cfg.SubmissionUsers)
	}
	if !cfg.SubmissionSenderID {
		t.Fatal("submission sender identity should be true")
	}
}

func TestLoadLogLevel(t *testing.T) {
	t.Setenv("MTA_LOG_LEVEL", "debug")
	cfg := Load()
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level=%q", cfg.LogLevel)
	}
}
