package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"gopkg.in/yaml.v3"
)

type EventWebhookConfig struct {
	Enabled           bool          `yaml:"enabled"`
	URL               string        `yaml:"url"`
	SecretFile        string        `yaml:"secret_file"`
	AllowHTTPLoopback bool          `yaml:"allow_http_loopback"`
	Timeout           time.Duration `yaml:"timeout"`
	ScanInterval      time.Duration `yaml:"scan_interval"`
	MaxPending        int           `yaml:"max_pending"`
	MaxAttempts       int           `yaml:"max_attempts"`
	MaxRetryDelay     time.Duration `yaml:"max_retry_delay"`
	Retention         time.Duration `yaml:"retention"`
}

func defaultEventWebhookConfig() EventWebhookConfig {
	return EventWebhookConfig{Timeout: 10 * time.Second, ScanInterval: time.Second, MaxPending: 100000, MaxAttempts: 12, MaxRetryDelay: time.Hour, Retention: 7 * 24 * time.Hour}
}

func (c *EventWebhookConfig) UnmarshalYAML(node *yaml.Node) error {
	type wire struct {
		Enabled           bool   `yaml:"enabled"`
		URL               string `yaml:"url"`
		SecretFile        string `yaml:"secret_file"`
		AllowHTTPLoopback bool   `yaml:"allow_http_loopback"`
		Timeout           string `yaml:"timeout"`
		ScanInterval      string `yaml:"scan_interval"`
		MaxPending        int    `yaml:"max_pending"`
		MaxAttempts       int    `yaml:"max_attempts"`
		MaxRetryDelay     string `yaml:"max_retry_delay"`
		Retention         string `yaml:"retention"`
	}
	var raw wire
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*c = defaultEventWebhookConfig()
	c.Enabled, c.URL, c.SecretFile, c.AllowHTTPLoopback = raw.Enabled, raw.URL, raw.SecretFile, raw.AllowHTTPLoopback
	if raw.MaxPending != 0 {
		c.MaxPending = raw.MaxPending
	}
	if raw.MaxAttempts != 0 {
		c.MaxAttempts = raw.MaxAttempts
	}
	for name, item := range map[string]struct {
		value  string
		target *time.Duration
	}{
		"timeout": {raw.Timeout, &c.Timeout}, "scan_interval": {raw.ScanInterval, &c.ScanInterval}, "max_retry_delay": {raw.MaxRetryDelay, &c.MaxRetryDelay}, "retention": {raw.Retention, &c.Retention},
	} {
		if item.value == "" {
			continue
		}
		parsed, err := time.ParseDuration(item.value)
		if err != nil {
			return fmt.Errorf("event_webhook.%s: %w", name, err)
		}
		*item.target = parsed
	}
	return nil
}

func (c EventWebhookConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("event_webhook.url must be an absolute endpoint without credentials, query or fragment")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowHTTPLoopback || ip == nil || !ip.IsLoopback() {
			return errors.New("event_webhook.url requires HTTPS; HTTP is limited to explicit literal loopback")
		}
	}
	if c.SecretFile == "" {
		return errors.New("event_webhook.secret_file is required")
	}
	if c.Timeout <= 0 || c.Timeout > 30*time.Second || c.ScanInterval <= 0 || c.ScanInterval > time.Minute || c.MaxPending < 1 || c.MaxPending > 1000000 || c.MaxAttempts < 1 || c.MaxAttempts > 100 || c.MaxRetryDelay <= 0 || c.MaxRetryDelay > 24*time.Hour || c.Retention <= 0 || c.Retention > 365*24*time.Hour {
		return errors.New("event_webhook limits are invalid")
	}
	return nil
}
