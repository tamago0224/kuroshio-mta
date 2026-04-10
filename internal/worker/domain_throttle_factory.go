package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func newDomainThrottle(cfg config.Config) (domainThrottle, error) {
	rules := parseDomainRules(cfg.DomainMaxConcurrentRules)
	switch strings.ToLower(strings.TrimSpace(cfg.DomainThrottleBackend)) {
	case "", "memory":
		return newLocalDomainThrottle(cfg.DomainMaxConcurrentDefault, rules, cfg.DomainAdaptiveThrottle, cfg.DomainTempFailThreshold, cfg.DomainPenaltyMax), nil
	case "redis":
		return newRedisDomainThrottle(redisDomainThrottleConfig{
			Addrs:      cfg.DomainThrottleRedisAddrs,
			Username:   cfg.DomainThrottleRedisUsername,
			Password:   cfg.DomainThrottleRedisPassword,
			DB:         cfg.DomainThrottleRedisDB,
			KeyPrefix:  cfg.DomainThrottleRedisKeyPrefix,
			DefLimit:   cfg.DomainMaxConcurrentDefault,
			Rules:      rules,
			Adaptive:   cfg.DomainAdaptiveThrottle,
			Threshold:  cfg.DomainTempFailThreshold,
			MaxPenalty: cfg.DomainPenaltyMax,
			LeaseTTL:   cfg.DialTimeout + cfg.SendTimeout + 30*time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported domain throttle backend %q", cfg.DomainThrottleBackend)
	}
}
