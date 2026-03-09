package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	ObservabilityAddr  string
	Hostname           string
	QueueDir           string
	TLSCertFile        string
	TLSKeyFile         string
	IngressRateLimit   int
	DNSBLZones         []string
	DNSBLCacheTTL      time.Duration
	MTASTSCacheTTL     time.Duration
	MTASTSFetchTimeout time.Duration
	MaxMessageBytes    int64
	WorkerCount        int
	MaxAttempts        int
	MaxRetryAge        time.Duration
	RetrySchedule      []time.Duration
	ScanInterval       time.Duration
	DialTimeout        time.Duration
	SendTimeout        time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:         env("MTA_LISTEN_ADDR", ":2525"),
		ObservabilityAddr:  env("MTA_OBSERVABILITY_ADDR", ":9090"),
		Hostname:           env("MTA_HOSTNAME", "orinoco.local"),
		QueueDir:           env("MTA_QUEUE_DIR", "./var/queue"),
		TLSCertFile:        env("MTA_TLS_CERT_FILE", ""),
		TLSKeyFile:         env("MTA_TLS_KEY_FILE", ""),
		IngressRateLimit:   envInt("MTA_INGRESS_RATE_LIMIT_PER_MINUTE", 100),
		DNSBLZones:         envCSV("MTA_DNSBL_ZONES", []string{}),
		DNSBLCacheTTL:      envDuration("MTA_DNSBL_CACHE_TTL", 5*time.Minute),
		MTASTSCacheTTL:     envDuration("MTA_MTA_STS_CACHE_TTL", time.Hour),
		MTASTSFetchTimeout: envDuration("MTA_MTA_STS_FETCH_TIMEOUT", 5*time.Second),
		MaxMessageBytes:    envInt64("MTA_MAX_MESSAGE_BYTES", 10*1024*1024),
		WorkerCount:        envInt("MTA_WORKER_COUNT", 4),
		MaxAttempts:        envInt("MTA_MAX_ATTEMPTS", 12),
		MaxRetryAge:        envDuration("MTA_MAX_RETRY_AGE", 5*24*time.Hour),
		RetrySchedule: envDurationList(
			"MTA_RETRY_SCHEDULE",
			[]time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour},
		),
		ScanInterval: envDuration("MTA_SCAN_INTERVAL", 5*time.Second),
		DialTimeout:  envDuration("MTA_DIAL_TIMEOUT", 8*time.Second),
		SendTimeout:  envDuration("MTA_SEND_TIMEOUT", 20*time.Second),
	}
}

func env(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envInt64(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envDurationList(k string, def []time.Duration) []time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return append([]time.Duration(nil), def...)
	}
	parts := strings.Split(v, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, p := range parts {
		d, err := time.ParseDuration(strings.TrimSpace(p))
		if err != nil {
			return append([]time.Duration(nil), def...)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return append([]time.Duration(nil), def...)
	}
	return out
}

func envCSV(k string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return append([]string(nil), def...)
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), def...)
	}
	return out
}
