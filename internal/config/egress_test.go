package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEgressConfigurationValidation(t *testing.T) {
	cfg := Config{
		EgressSources:     []EgressSource{{Name: "primary", IP: "127.0.0.1", EHLO: "mail.example.test", Weight: 2, DailyLimit: 100}},
		EgressPools:       []EgressPool{{Name: "transactional", Sources: []string{"primary"}}},
		EgressTenantPools: map[string]string{"alice@example.test": "transactional"},
		DeliveryRoutes:    []DeliveryRoute{{Name: "external", Domains: []string{"example.net"}, Mode: "mx", EgressPool: "transactional"}},
	}
	if err := cfg.validateEgress(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.validateDeliveryRoutes(); err != nil {
		t.Fatal(err)
	}
	cfg.EgressSources[0].IP = "not-an-ip"
	if err := cfg.validateEgress(); err == nil {
		t.Fatal("invalid source IP accepted")
	}
}

func TestEgressRejectsEmptyPoolAndUnknownRoutePool(t *testing.T) {
	if err := (Config{EgressPools: []EgressPool{{Name: "empty"}}}).validateEgress(); err == nil {
		t.Fatal("empty pool accepted")
	}
	cfg := Config{DeliveryRoutes: []DeliveryRoute{{Name: "bad", Domains: []string{"example.net"}, Mode: "mx", EgressPool: "missing"}}}
	if err := cfg.validateDeliveryRoutes(); err == nil {
		t.Fatal("unknown route pool accepted")
	}
}

func TestEgressConfigurationLoadsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `egress_sources:
  - name: primary
    ip: 127.0.0.1
    ehlo: mail.example.test
    weight: 2
    daily_limit: 100
egress_pools:
  - name: transactional
    sources: [primary]
egress_tenant_pools:
  alice@example.test: transactional
delivery_routes:
  - name: external
    domains: [example.net]
    mode: mx
    egress_pool: transactional
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EgressSources) != 1 || cfg.EgressSources[0].DailyLimit != 100 || cfg.DeliveryRoutes[0].EgressPool != "transactional" || cfg.EgressTenantPools["alice@example.test"] != "transactional" {
		t.Fatalf("egress configuration = %+v %+v %+v", cfg.EgressSources, cfg.EgressPools, cfg.DeliveryRoutes)
	}
}
