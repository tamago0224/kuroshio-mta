package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func mustLoadForTest(t *testing.T) Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return cfg
}

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
	cfg := mustLoadForTest(t)
	if cfg.RateLimitRules != "connect:ip:10:1m" {
		t.Fatalf("unexpected rules: %q", cfg.RateLimitRules)
	}
}

func TestLoadSubmissionConfig(t *testing.T) {
	t.Setenv("MTA_SUBMISSION_ADDR", ":587")
	t.Setenv("MTA_SUBMISSION_AUTH_REQUIRED", "true")
	t.Setenv("MTA_SUBMISSION_USERS", "alice@example.com:s3cr3t")
	t.Setenv("MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY", "true")

	cfg := mustLoadForTest(t)
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

func TestLoadSubmissionUsersFromFile(t *testing.T) {
	fp := t.TempDir() + "/submission_users.txt"
	if err := os.WriteFile(fp, []byte("fileuser@example.com:filepass\n"), 0o644); err != nil {
		t.Fatalf("write users file: %v", err)
	}
	t.Setenv("MTA_SUBMISSION_USERS", "envuser@example.com:envpass")
	t.Setenv("MTA_SUBMISSION_USERS_FILE", fp)

	cfg := mustLoadForTest(t)
	if cfg.SubmissionUsers != "fileuser@example.com:filepass" {
		t.Fatalf("submission users should prefer file value, got=%q", cfg.SubmissionUsers)
	}
}

func TestLoadLogLevel(t *testing.T) {
	t.Setenv("MTA_LOG_LEVEL", "debug")
	cfg := mustLoadForTest(t)
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level=%q", cfg.LogLevel)
	}
}

func TestLoadDKIMSigningConfig(t *testing.T) {
	t.Setenv("MTA_DKIM_SIGN_DOMAIN", "example.com")
	t.Setenv("MTA_DKIM_SIGN_SELECTOR", "s1")
	t.Setenv("MTA_DKIM_PRIVATE_KEY_FILE", "/tmp/dkim.pem")
	t.Setenv("MTA_DKIM_SIGN_HEADERS", "from:to:subject")
	cfg := mustLoadForTest(t)
	if cfg.DKIMSignDomain != "example.com" || cfg.DKIMSignSelector != "s1" {
		t.Fatalf("unexpected dkim config: domain=%q selector=%q", cfg.DKIMSignDomain, cfg.DKIMSignSelector)
	}
	if cfg.DKIMPrivateKeyFile != "/tmp/dkim.pem" {
		t.Fatalf("key file=%q", cfg.DKIMPrivateKeyFile)
	}
	if cfg.DKIMSignHeaders != "from:to:subject" {
		t.Fatalf("headers=%q", cfg.DKIMSignHeaders)
	}
}

func TestLoadSPFPolicyConfig(t *testing.T) {
	t.Setenv("MTA_SPF_HELO_POLICY", "off")
	t.Setenv("MTA_SPF_MAILFROM_POLICY", "advisory")
	cfg := mustLoadForTest(t)
	if cfg.SPFHeloPolicy != "off" {
		t.Fatalf("helo policy=%q", cfg.SPFHeloPolicy)
	}
	if cfg.SPFMailFromPolicy != "advisory" {
		t.Fatalf("mailfrom policy=%q", cfg.SPFMailFromPolicy)
	}

	t.Setenv("MTA_SPF_HELO_POLICY", "invalid")
	t.Setenv("MTA_SPF_MAILFROM_POLICY", "invalid")
	cfg = mustLoadForTest(t)
	if cfg.SPFHeloPolicy != "advisory" {
		t.Fatalf("helo invalid should fallback, got=%q", cfg.SPFHeloPolicy)
	}
	if cfg.SPFMailFromPolicy != "advisory" {
		t.Fatalf("mailfrom invalid should fallback, got=%q", cfg.SPFMailFromPolicy)
	}
}

func TestLoadARCFailurePolicyConfig(t *testing.T) {
	t.Setenv("MTA_ARC_FAILURE_POLICY", "reject")
	cfg := mustLoadForTest(t)
	if cfg.ARCFailurePolicy != "reject" {
		t.Fatalf("arc policy=%q", cfg.ARCFailurePolicy)
	}

	t.Setenv("MTA_ARC_FAILURE_POLICY", "invalid")
	cfg = mustLoadForTest(t)
	if cfg.ARCFailurePolicy != "accept" {
		t.Fatalf("arc invalid should fallback, got=%q", cfg.ARCFailurePolicy)
	}
}

func TestLoadDomainThrottleConfig(t *testing.T) {
	t.Setenv("MTA_DOMAIN_MAX_CONCURRENT_DEFAULT", "4")
	t.Setenv("MTA_DOMAIN_MAX_CONCURRENT_RULES", "gmail.com:2,yahoo.com:1")
	t.Setenv("MTA_DOMAIN_ADAPTIVE_THROTTLE", "true")
	t.Setenv("MTA_DOMAIN_TEMPFAIL_THRESHOLD", "0.5")
	t.Setenv("MTA_DOMAIN_PENALTY_MAX", "3s")
	cfg := mustLoadForTest(t)
	if cfg.DomainMaxConcurrentDefault != 4 {
		t.Fatalf("default concurrency=%d", cfg.DomainMaxConcurrentDefault)
	}
	if cfg.DomainMaxConcurrentRules != "gmail.com:2,yahoo.com:1" {
		t.Fatalf("rules=%q", cfg.DomainMaxConcurrentRules)
	}
	if !cfg.DomainAdaptiveThrottle {
		t.Fatal("adaptive throttle should be true")
	}
	if cfg.DomainTempFailThreshold != 0.5 {
		t.Fatalf("threshold=%v", cfg.DomainTempFailThreshold)
	}
	if cfg.DomainPenaltyMax != 3*time.Second {
		t.Fatalf("penalty max=%s", cfg.DomainPenaltyMax)
	}
}

func TestLoadRetentionConfig(t *testing.T) {
	t.Setenv("MTA_DATA_RETENTION_SENT", "720h")
	t.Setenv("MTA_DATA_RETENTION_DLQ", "2160h")
	t.Setenv("MTA_DATA_RETENTION_POISON", "4320h")
	t.Setenv("MTA_RETENTION_SWEEP_INTERVAL", "30m")
	cfg := mustLoadForTest(t)
	if cfg.DataRetentionSent != 720*time.Hour {
		t.Fatalf("retention sent=%s", cfg.DataRetentionSent)
	}
	if cfg.DataRetentionDLQ != 2160*time.Hour {
		t.Fatalf("retention dlq=%s", cfg.DataRetentionDLQ)
	}
	if cfg.DataRetentionPoison != 4320*time.Hour {
		t.Fatalf("retention poison=%s", cfg.DataRetentionPoison)
	}
	if cfg.RetentionSweepInterval != 30*time.Minute {
		t.Fatalf("retention sweep=%s", cfg.RetentionSweepInterval)
	}
}

func TestLoadDANEDNSSECTrustModel(t *testing.T) {
	t.Setenv("MTA_DANE_DNSSEC_TRUST_MODEL", "insecure_allow_unsigned")
	cfg := mustLoadForTest(t)
	if cfg.DANEDNSSECTrustModel != "insecure_allow_unsigned" {
		t.Fatalf("trust model=%q", cfg.DANEDNSSECTrustModel)
	}

	t.Setenv("MTA_DANE_DNSSEC_TRUST_MODEL", "invalid")
	cfg = mustLoadForTest(t)
	if cfg.DANEDNSSECTrustModel != "ad_required" {
		t.Fatalf("invalid trust model should fallback, got=%q", cfg.DANEDNSSECTrustModel)
	}
}

func TestLoadYAMLConfig(t *testing.T) {
	fp := t.TempDir() + "/config.yaml"
	content := strings.TrimSpace(`
listen_addr: ":2526"
submission_addr: ":587"
submission_auth_required: false
submission_users: "yaml-user@example.com:yaml-pass"
log_level: debug
hostname: yaml.local
queue_backend: kafka
kafka_brokers:
  - kafka-1:9092
  - kafka-2:9092
dnsbl_zones:
  - zen.example.org
  - bl.example.net
dnsbl_cache_ttl: 10m
retry_schedule:
  - 1m
  - 15m
max_retry_age: 48h
worker_count: 8
domain_tempfail_threshold: 0.4
`)
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("MTA_CONFIG_FILE", fp)

	cfg := mustLoadForTest(t)
	if cfg.ListenAddr != ":2526" {
		t.Fatalf("listen addr=%q", cfg.ListenAddr)
	}
	if cfg.SubmissionAddr != ":587" {
		t.Fatalf("submission addr=%q", cfg.SubmissionAddr)
	}
	if cfg.SubmissionAuth {
		t.Fatal("submission auth should be false from yaml")
	}
	if cfg.SubmissionUsers != "yaml-user@example.com:yaml-pass" {
		t.Fatalf("submission users=%q", cfg.SubmissionUsers)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level=%q", cfg.LogLevel)
	}
	if cfg.Hostname != "yaml.local" {
		t.Fatalf("hostname=%q", cfg.Hostname)
	}
	if cfg.QueueBackend != "kafka" {
		t.Fatalf("queue backend=%q", cfg.QueueBackend)
	}
	if len(cfg.KafkaBrokers) != 2 || cfg.KafkaBrokers[0] != "kafka-1:9092" || cfg.KafkaBrokers[1] != "kafka-2:9092" {
		t.Fatalf("kafka brokers=%v", cfg.KafkaBrokers)
	}
	if len(cfg.DNSBLZones) != 2 || cfg.DNSBLZones[0] != "zen.example.org" || cfg.DNSBLZones[1] != "bl.example.net" {
		t.Fatalf("dnsbl zones=%v", cfg.DNSBLZones)
	}
	if cfg.DNSBLCacheTTL != 10*time.Minute {
		t.Fatalf("dnsbl cache ttl=%s", cfg.DNSBLCacheTTL)
	}
	if len(cfg.RetrySchedule) != 2 || cfg.RetrySchedule[0] != time.Minute || cfg.RetrySchedule[1] != 15*time.Minute {
		t.Fatalf("retry schedule=%v", cfg.RetrySchedule)
	}
	if cfg.MaxRetryAge != 48*time.Hour {
		t.Fatalf("max retry age=%s", cfg.MaxRetryAge)
	}
	if cfg.WorkerCount != 8 {
		t.Fatalf("worker count=%d", cfg.WorkerCount)
	}
	if cfg.DomainTempFailThreshold != 0.4 {
		t.Fatalf("threshold=%v", cfg.DomainTempFailThreshold)
	}
}

func TestLoadYAMLConfigEnvOverrides(t *testing.T) {
	fp := t.TempDir() + "/config.yaml"
	content := strings.TrimSpace(`
listen_addr: ":2526"
log_level: info
queue_backend: local
retry_schedule:
  - 1m
  - 15m
`)
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("MTA_CONFIG_FILE", fp)
	t.Setenv("MTA_LISTEN_ADDR", ":2600")
	t.Setenv("MTA_LOG_LEVEL", "warn")
	t.Setenv("MTA_QUEUE_BACKEND", "kafka")
	t.Setenv("MTA_RETRY_SCHEDULE", "5m,30m")

	cfg := mustLoadForTest(t)
	if cfg.ListenAddr != ":2600" {
		t.Fatalf("listen addr=%q", cfg.ListenAddr)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("log level=%q", cfg.LogLevel)
	}
	if cfg.QueueBackend != "kafka" {
		t.Fatalf("queue backend=%q", cfg.QueueBackend)
	}
	if len(cfg.RetrySchedule) != 2 || cfg.RetrySchedule[0] != 5*time.Minute || cfg.RetrySchedule[1] != 30*time.Minute {
		t.Fatalf("retry schedule=%v", cfg.RetrySchedule)
	}
}

func TestLoadInvalidYAMLDuration(t *testing.T) {
	fp := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(fp, []byte("dnsbl_cache_ttl: definitely-not-a-duration\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("MTA_CONFIG_FILE", fp)

	_, err := Load()
	if err == nil {
		t.Fatal("expected yaml duration parse error")
	}
	if !strings.Contains(err.Error(), "dnsbl_cache_ttl") {
		t.Fatalf("unexpected error: %v", err)
	}
}
