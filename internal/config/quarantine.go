package config

import (
	"errors"
	"strings"
	"time"
)

// QuarantineConfig enables local, persistent policy holds instead of headers only.
type QuarantineConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Retention time.Duration `yaml:"retention"`
}

func (c QuarantineConfig) Validate(backend string) error {
	if c.Retention < 0 || c.Retention > 365*24*time.Hour {
		return errors.New("quarantine.retention must be within 0..365 days")
	}
	if !c.Enabled {
		return nil
	}
	if c.Retention == 0 {
		return errors.New("quarantine.retention must be positive when enabled")
	}
	if backend != "" && !strings.EqualFold(strings.TrimSpace(backend), "local") {
		return errors.New("quarantine requires local queue backend")
	}
	return nil
}
