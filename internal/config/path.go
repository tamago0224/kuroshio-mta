package config

import (
	"os"
	"strings"
)

func resolveConfigPath(explicitPath string) (string, string, bool) {
	if v := strings.TrimSpace(explicitPath); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, v, true
		}
		return "", v, true
	}
	if v := strings.TrimSpace(os.Getenv("MTA_CONFIG_FILE")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, v, true
		}
		return "", v, true
	}
	for _, candidate := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, "", false
		}
	}
	return "", "", false
}
