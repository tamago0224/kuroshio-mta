package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDNSCacheConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("dns_cache_enabled: true\ndns_cache_positive_ttl: 2m\ndns_cache_negative_ttl: 10s\ndns_cache_capacity: 250\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DNSCacheEnabled || cfg.DNSCachePositiveTTL != 2*time.Minute || cfg.DNSCacheNegativeTTL != 10*time.Second || cfg.DNSCacheCapacity != 250 {
		t.Fatalf("DNS cache config = %+v", cfg)
	}
}

func TestDNSCacheRejectsUnsafeBounds(t *testing.T) {
	for _, cfg := range []Config{
		{DNSCachePositiveTTL: -time.Second},
		{DNSCachePositiveTTL: 25 * time.Hour},
		{DNSCacheNegativeTTL: 2 * time.Hour},
		{DNSCacheCapacity: 1000001},
	} {
		if err := cfg.validateDNSCache(); err == nil {
			t.Fatalf("invalid DNS cache accepted: %+v", cfg)
		}
	}
}
