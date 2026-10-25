package config

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestHTTPInjectionConfigYAMLAndValidation(t *testing.T) {
	var cfg HTTPInjectionConfig
	if err := yaml.Unmarshal([]byte("enabled: true\nlisten_addr: 127.0.0.1:9443\nidempotency_ttl: 2h\nrate_limit_per_minute: 25\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:9443" || cfg.IdempotencyTTL != 2*time.Hour || cfg.RateLimitPerMinute != 25 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if err := cfg.Validate("cert.pem", "key.pem"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate("", ""); err == nil || !strings.Contains(err.Error(), "tls_cert_file") {
		t.Fatalf("expected TLS validation error, got %v", err)
	}
}
