package config

import (
	"errors"
	"strings"
)

type QueueFairnessConfig struct {
	Enabled           bool              `yaml:"enabled"`
	PrefetchFactor    int               `yaml:"prefetch_factor"`
	MaxTenants        int               `yaml:"max_tenants"`
	TenantMaxPerBatch int               `yaml:"tenant_max_per_batch"`
	ClassWeights      map[string]int    `yaml:"class_weights"`
	ClassMaxPerBatch  map[string]int    `yaml:"class_max_per_batch"`
	TenantClasses     map[string]string `yaml:"tenant_classes"`
}

func (c QueueFairnessConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.PrefetchFactor < 2 || c.PrefetchFactor > 64 || c.MaxTenants < 2 || c.MaxTenants > 4096 || len(c.ClassWeights) == 0 || len(c.ClassWeights) > 16 || len(c.TenantClasses) > c.MaxTenants {
		return errors.New("invalid queue_fairness limits")
	}
	for class, weight := range c.ClassWeights {
		if class != strings.ToLower(strings.TrimSpace(class)) || class == "" || weight < 1 || weight > 100 {
			return errors.New("invalid queue_fairness class weight")
		}
	}
	for tenant, class := range c.TenantClasses {
		if tenant != strings.ToLower(strings.TrimSpace(tenant)) || tenant == "" || c.ClassWeights[class] == 0 {
			return errors.New("invalid queue_fairness tenant mapping")
		}
	}
	if c.TenantMaxPerBatch < 0 {
		return errors.New("invalid queue_fairness tenant batch limit")
	}
	for class, limit := range c.ClassMaxPerBatch {
		if c.ClassWeights[class] == 0 || limit < 1 {
			return errors.New("invalid queue_fairness class batch limit")
		}
	}
	return nil
}
