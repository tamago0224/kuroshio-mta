package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

type DeliveryHistoryConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Retention time.Duration `yaml:"retention"`
	MaxRows   int           `yaml:"max_rows"`
}

func defaultDeliveryHistoryConfig() DeliveryHistoryConfig {
	return DeliveryHistoryConfig{Retention: 30 * 24 * time.Hour, MaxRows: 100_000}
}

func (c *DeliveryHistoryConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Enabled   bool   `yaml:"enabled"`
		Retention string `yaml:"retention"`
		MaxRows   int    `yaml:"max_rows"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*c = defaultDeliveryHistoryConfig()
	c.Enabled = raw.Enabled
	if raw.Retention != "" {
		retention, err := time.ParseDuration(raw.Retention)
		if err != nil {
			return fmt.Errorf("delivery_history.retention: %w", err)
		}
		c.Retention = retention
	}
	if raw.MaxRows != 0 {
		c.MaxRows = raw.MaxRows
	}
	return nil
}

func (c DeliveryHistoryConfig) Validate() error {
	if c.Retention <= 0 || c.Retention > 365*24*time.Hour {
		return fmt.Errorf("delivery_history.retention must be between 1ns and 365d")
	}
	if c.MaxRows < 1 || c.MaxRows > 10_000_000 {
		return fmt.Errorf("delivery_history.max_rows must be between 1 and 10000000")
	}
	return nil
}
