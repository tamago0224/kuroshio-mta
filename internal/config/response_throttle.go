package config

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var responseThrottleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var responseThrottleStatusPattern = regexp.MustCompile(`^4\.[0-9]{1,3}\.[0-9]{1,3}$`)

type ResponseThrottleConfig struct {
	Enabled bool                   `yaml:"enabled"`
	DryRun  bool                   `yaml:"dry_run"`
	Rules   []ResponseThrottleRule `yaml:"rules"`
}

type ResponseThrottleRule struct {
	ID             string        `yaml:"id"`
	Domains        []string      `yaml:"domains"`
	SMTPCode       int           `yaml:"smtp_code"`
	EnhancedStatus string        `yaml:"enhanced_status"`
	Stage          string        `yaml:"stage"`
	Threshold      int           `yaml:"threshold"`
	Window         time.Duration `yaml:"window"`
	Pause          time.Duration `yaml:"pause"`
}

func (c *ResponseThrottleConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Enabled bool                   `yaml:"enabled"`
		DryRun  *bool                  `yaml:"dry_run"`
		Rules   []ResponseThrottleRule `yaml:"rules"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	c.Enabled, c.DryRun, c.Rules = raw.Enabled, true, raw.Rules
	if raw.DryRun != nil {
		c.DryRun = *raw.DryRun
	}
	return nil
}

func (r *ResponseThrottleRule) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		ID             string   `yaml:"id"`
		Domains        []string `yaml:"domains"`
		SMTPCode       int      `yaml:"smtp_code"`
		EnhancedStatus string   `yaml:"enhanced_status"`
		Stage          string   `yaml:"stage"`
		Threshold      int      `yaml:"threshold"`
		Window         string   `yaml:"window"`
		Pause          string   `yaml:"pause"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	window, err := time.ParseDuration(raw.Window)
	if err != nil {
		return err
	}
	pause, err := time.ParseDuration(raw.Pause)
	if err != nil {
		return err
	}
	*r = ResponseThrottleRule{ID: raw.ID, Domains: raw.Domains, SMTPCode: raw.SMTPCode, EnhancedStatus: raw.EnhancedStatus, Stage: raw.Stage, Threshold: raw.Threshold, Window: window, Pause: pause}
	return nil
}

func (c ResponseThrottleConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.Rules) == 0 || len(c.Rules) > 64 {
		return errors.New("response_throttle requires 1..64 rules")
	}
	seen := map[string]bool{}
	for _, rule := range c.Rules {
		id := strings.TrimSpace(rule.ID)
		if !responseThrottleIDPattern.MatchString(id) || seen[id] || rule.SMTPCode < 400 || rule.SMTPCode > 499 || rule.Threshold < 1 || rule.Threshold > 10000 || rule.Window <= 0 || rule.Window > 24*time.Hour || rule.Pause <= 0 || rule.Pause > 24*time.Hour {
			return errors.New("invalid response_throttle rule")
		}
		if rule.EnhancedStatus != "" && !responseThrottleStatusPattern.MatchString(rule.EnhancedStatus) {
			return errors.New("invalid response_throttle enhanced status")
		}
		switch strings.ToLower(rule.Stage) {
		case "", "connect", "greeting", "ehlo", "tls", "auth", "mail", "rcpt", "data", "dns":
		default:
			return errors.New("invalid response_throttle stage")
		}
		seen[id] = true
	}
	return nil
}
