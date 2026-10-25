package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type HTTPInjectionConfig struct {
	Enabled            bool          `yaml:"enabled"`
	ListenAddr         string        `yaml:"listen_addr"`
	IdempotencyTTL     time.Duration `yaml:"idempotency_ttl"`
	RateLimitPerMinute int           `yaml:"rate_limit_per_minute"`
}

func (c *HTTPInjectionConfig) UnmarshalYAML(node *yaml.Node) error {
	type rawConfig struct {
		Enabled            bool   `yaml:"enabled"`
		ListenAddr         string `yaml:"listen_addr"`
		IdempotencyTTL     string `yaml:"idempotency_ttl"`
		RateLimitPerMinute int    `yaml:"rate_limit_per_minute"`
	}
	var raw rawConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}
	defaults := defaultHTTPInjectionConfig()
	*c = defaults
	c.Enabled = raw.Enabled
	if raw.ListenAddr != "" {
		c.ListenAddr = raw.ListenAddr
	}
	if raw.IdempotencyTTL != "" {
		ttl, err := time.ParseDuration(raw.IdempotencyTTL)
		if err != nil {
			return fmt.Errorf("invalid http_injection.idempotency_ttl: %w", err)
		}
		c.IdempotencyTTL = ttl
	}
	if raw.RateLimitPerMinute != 0 {
		c.RateLimitPerMinute = raw.RateLimitPerMinute
	}
	return nil
}

func defaultHTTPInjectionConfig() HTTPInjectionConfig {
	return HTTPInjectionConfig{
		ListenAddr:         "127.0.0.1:8443",
		IdempotencyTTL:     24 * time.Hour,
		RateLimitPerMinute: 60,
	}
}

func (c HTTPInjectionConfig) Validate(certFile, keyFile string) error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("http_injection.listen_addr is required when enabled")
	}
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" {
		return fmt.Errorf("http_injection requires tls_cert_file and tls_key_file")
	}
	if c.IdempotencyTTL < time.Minute || c.IdempotencyTTL > 30*24*time.Hour {
		return fmt.Errorf("http_injection.idempotency_ttl must be between 1m and 720h")
	}
	if c.RateLimitPerMinute < 1 || c.RateLimitPerMinute > 100000 {
		return fmt.Errorf("http_injection.rate_limit_per_minute must be between 1 and 100000")
	}
	return nil
}
