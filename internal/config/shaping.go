package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MXShapingConfig is opt-in; nested options are configured in YAML.
type MXShapingConfig struct {
	Enabled    bool              `yaml:"enabled"`
	Backend    string            `yaml:"backend"`
	CacheTTL   time.Duration     `yaml:"cache_ttl"`
	RetryDelay time.Duration     `yaml:"retry_delay"`
	Default    ShapingLimits     `yaml:"default"`
	Providers  []ShapingProvider `yaml:"providers"`
}

type ShapingLimits struct {
	MessagesPerMinute    int `yaml:"messages_per_minute"`
	MessageBurst         int `yaml:"message_burst"`
	ConnectionsPerMinute int `yaml:"connections_per_minute"`
	ConnectionBurst      int `yaml:"connection_burst"`
	MaxConnections       int `yaml:"max_connections"`
}

type ShapingProvider struct {
	Name       string        `yaml:"name"`
	Domains    []string      `yaml:"domains"`
	MXSuffixes []string      `yaml:"mx_suffixes"`
	Limits     ShapingLimits `yaml:"limits"`
}

func (c MXShapingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Backend != "" && c.Backend != "memory" && c.Backend != "redis" {
		return errors.New("mx_shaping backend must be memory or redis")
	}
	if c.CacheTTL < 0 || c.CacheTTL > time.Hour || c.RetryDelay < 0 || c.RetryDelay > time.Hour {
		return errors.New("mx_shaping cache_ttl/retry_delay must be between 0 and 1h")
	}
	limits := []ShapingLimits{c.Default}
	names := map[string]bool{}
	domains := map[string]bool{}
	for _, p := range c.Providers {
		if p.Name == "" || names[p.Name] {
			return errors.New("mx_shaping provider names must be nonempty and unique")
		}
		names[p.Name] = true
		for _, d := range p.Domains {
			d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
			if d == "" || domains[d] {
				return errors.New("mx_shaping domains must be nonempty and unique")
			}
			domains[d] = true
		}
		for _, suffix := range p.MXSuffixes {
			if strings.Trim(suffix, ". ") == "" {
				return errors.New("mx_shaping MX suffix must be nonempty")
			}
		}
		limits = append(limits, p.Limits)
	}
	for _, l := range limits {
		for _, n := range []int{l.MessagesPerMinute, l.MessageBurst, l.ConnectionsPerMinute, l.ConnectionBurst, l.MaxConnections} {
			if n < 0 || n > 1000000 {
				return fmt.Errorf("mx_shaping limits must be between 0 and 1000000")
			}
		}
	}
	return nil
}
