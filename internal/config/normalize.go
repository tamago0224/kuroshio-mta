package config

import "strings"

func normalizeEnum(v, def string, allowed []string) string {
	value := strings.TrimSpace(strings.ToLower(v))
	if value == "" {
		return def
	}
	for _, candidate := range allowed {
		if value == strings.ToLower(strings.TrimSpace(candidate)) {
			return value
		}
	}
	return def
}

func normalizeSampleRatio(v, def float64) float64 {
	if v < 0 || v > 1 {
		return def
	}
	return v
}
