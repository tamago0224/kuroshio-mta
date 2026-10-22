package config

import (
	"errors"
	"net"
	"strings"
)

type EgressSource struct {
	Name       string `yaml:"name"`
	IP         string `yaml:"ip"`
	EHLO       string `yaml:"ehlo"`
	Weight     int    `yaml:"weight"`
	DailyLimit int    `yaml:"daily_limit"`
	Stopped    bool   `yaml:"stopped"`
}
type EgressPool struct {
	Name    string   `yaml:"name"`
	Sources []string `yaml:"sources"`
}

func (c Config) validateEgress() error {
	sources := map[string]bool{}
	for _, source := range c.EgressSources {
		if source.Name == "" || sources[source.Name] || net.ParseIP(source.IP) == nil || !validLocalDomain(strings.ToLower(strings.TrimSuffix(source.EHLO, "."))) || source.Weight < 1 || source.Weight > 100 || source.DailyLimit < 0 {
			return errors.New("invalid egress source")
		}
		sources[source.Name] = true
	}
	pools := map[string]bool{}
	for _, pool := range c.EgressPools {
		if pool.Name == "" || pools[pool.Name] || len(pool.Sources) == 0 {
			return errors.New("invalid egress pool")
		}
		pools[pool.Name] = true
		for _, name := range pool.Sources {
			if !sources[name] {
				return errors.New("egress pool references unknown source")
			}
		}
	}
	for tenant, pool := range c.EgressTenantPools {
		if strings.TrimSpace(tenant) == "" || !pools[pool] {
			return errors.New("invalid egress tenant pool")
		}
	}
	return nil
}
