package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// SMTPConnectionPoolConfig controls opt-in outbound SMTP connection reuse.
type SMTPConnectionPoolConfig struct {
	Enabled           bool          `yaml:"enabled"`
	MaxConnections    int           `yaml:"max_connections"`
	MaxConnectionsKey int           `yaml:"max_connections_per_key"`
	MaxIdle           int           `yaml:"max_idle"`
	MaxIdleKey        int           `yaml:"max_idle_per_key"`
	IdleTimeout       time.Duration `yaml:"idle_timeout"`
	MaxLifetime       time.Duration `yaml:"max_lifetime"`
	MaxDeliveries     int           `yaml:"max_deliveries"`
}

func (c *SMTPConnectionPoolConfig) UnmarshalYAML(node *yaml.Node) error {
	type wire struct {
		Enabled           bool   `yaml:"enabled"`
		MaxConnections    int    `yaml:"max_connections"`
		MaxConnectionsKey int    `yaml:"max_connections_per_key"`
		MaxIdle           *int   `yaml:"max_idle"`
		MaxIdleKey        *int   `yaml:"max_idle_per_key"`
		IdleTimeout       string `yaml:"idle_timeout"`
		MaxLifetime       string `yaml:"max_lifetime"`
		MaxDeliveries     int    `yaml:"max_deliveries"`
	}
	var raw wire
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*c = defaultSMTPConnectionPoolConfig()
	c.Enabled = raw.Enabled
	if raw.MaxConnections != 0 {
		c.MaxConnections = raw.MaxConnections
	}
	if raw.MaxConnectionsKey != 0 {
		c.MaxConnectionsKey = raw.MaxConnectionsKey
	}
	if raw.MaxIdle != nil {
		c.MaxIdle = *raw.MaxIdle
	}
	if raw.MaxIdleKey != nil {
		c.MaxIdleKey = *raw.MaxIdleKey
	}
	if raw.MaxDeliveries != 0 {
		c.MaxDeliveries = raw.MaxDeliveries
	}
	var err error
	if raw.IdleTimeout != "" {
		c.IdleTimeout, err = time.ParseDuration(raw.IdleTimeout)
		if err != nil {
			return fmt.Errorf("smtp_connection_pool.idle_timeout: %w", err)
		}
	}
	if raw.MaxLifetime != "" {
		c.MaxLifetime, err = time.ParseDuration(raw.MaxLifetime)
		if err != nil {
			return fmt.Errorf("smtp_connection_pool.max_lifetime: %w", err)
		}
	}
	return nil
}

func defaultSMTPConnectionPoolConfig() SMTPConnectionPoolConfig {
	return SMTPConnectionPoolConfig{MaxConnections: 64, MaxConnectionsKey: 4, MaxIdle: 32, MaxIdleKey: 2, IdleTimeout: 30 * time.Second, MaxLifetime: 5 * time.Minute, MaxDeliveries: 100}
}

func (c SMTPConnectionPoolConfig) Validate() error {
	if c.MaxConnections < 1 || c.MaxConnections > 100000 || c.MaxConnectionsKey < 1 || c.MaxConnectionsKey > c.MaxConnections {
		return fmt.Errorf("smtp_connection_pool connection limits are invalid")
	}
	if c.MaxIdle < 0 || c.MaxIdle > c.MaxConnections || c.MaxIdleKey < 0 || c.MaxIdleKey > c.MaxConnectionsKey {
		return fmt.Errorf("smtp_connection_pool idle limits are invalid")
	}
	if c.IdleTimeout <= 0 || c.IdleTimeout > time.Hour || c.MaxLifetime <= 0 || c.MaxLifetime > 24*time.Hour {
		return fmt.Errorf("smtp_connection_pool time limits are invalid")
	}
	if c.MaxDeliveries < 1 || c.MaxDeliveries > 1000000 {
		return fmt.Errorf("smtp_connection_pool max_deliveries is invalid")
	}
	return nil
}
