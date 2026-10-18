package config

import (
	"errors"
	"time"
)

// SMTPLimitsConfig bounds resources independently for each receiving listener.
// Zero selects the documented default; negative values are invalid.
type SMTPLimitsConfig struct {
	MaxConnections      int           `yaml:"max_connections"`
	MaxConnectionsPerIP int           `yaml:"max_connections_per_ip"`
	MaxRecipients       int           `yaml:"max_recipients"`
	MaxMessages         int           `yaml:"max_messages"`
	MaxBytes            int64         `yaml:"max_bytes"`
	MaxAuthFailures     int           `yaml:"max_auth_failures"`
	MaxRcptFailures     int           `yaml:"max_rcpt_failures"`
	IdleTimeout         time.Duration `yaml:"idle_timeout"`
	MaxLifetime         time.Duration `yaml:"max_lifetime"`
}

func (c SMTPLimitsConfig) Validate() error {
	values := []int64{
		int64(c.MaxConnections), int64(c.MaxConnectionsPerIP), int64(c.MaxRecipients),
		int64(c.MaxMessages), c.MaxBytes, int64(c.MaxAuthFailures), int64(c.MaxRcptFailures),
		int64(c.IdleTimeout), int64(c.MaxLifetime),
	}
	for _, n := range values {
		if n < 0 {
			return errors.New("smtp_limits values must be non-negative")
		}
	}
	return nil
}

func (c SMTPLimitsConfig) WithDefaults() SMTPLimitsConfig {
	if c.MaxConnections == 0 {
		c.MaxConnections = 1024
	}
	if c.MaxConnectionsPerIP == 0 {
		c.MaxConnectionsPerIP = 32
	}
	if c.MaxRecipients == 0 {
		c.MaxRecipients = 100
	}
	if c.MaxMessages == 0 {
		c.MaxMessages = 100
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 100 * 1024 * 1024
	}
	if c.MaxAuthFailures == 0 {
		c.MaxAuthFailures = 5
	}
	if c.MaxRcptFailures == 0 {
		c.MaxRcptFailures = 20
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 5 * time.Minute
	}
	if c.MaxLifetime == 0 {
		c.MaxLifetime = 10 * time.Minute
	}
	return c
}
