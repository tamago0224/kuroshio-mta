package config

import "errors"

// DeliveryPauseConfig is configured in YAML; Redis uses domain_throttle_redis_*.
type DeliveryPauseConfig struct {
	Enabled bool   `yaml:"enabled"`
	Backend string `yaml:"backend"`
}

func (c DeliveryPauseConfig) Validate(addrs []string) error {
	if !c.Enabled {
		return nil
	}
	if c.Backend != "" && c.Backend != "local" && c.Backend != "redis" {
		return errors.New("delivery_pause.backend must be local or redis")
	}
	if c.Backend == "redis" && len(addrs) == 0 {
		return errors.New("delivery_pause Redis requires domain_throttle_redis_addrs")
	}
	return nil
}
