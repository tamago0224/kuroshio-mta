package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr      string
	Hostname        string
	QueueDir        string
	TLSCertFile     string
	TLSKeyFile      string
	MaxMessageBytes int64
	WorkerCount     int
	ScanInterval    time.Duration
	DialTimeout     time.Duration
	SendTimeout     time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:      env("MTA_LISTEN_ADDR", ":2525"),
		Hostname:        env("MTA_HOSTNAME", "orinoco.local"),
		QueueDir:        env("MTA_QUEUE_DIR", "./var/queue"),
		TLSCertFile:     env("MTA_TLS_CERT_FILE", ""),
		TLSKeyFile:      env("MTA_TLS_KEY_FILE", ""),
		MaxMessageBytes: envInt64("MTA_MAX_MESSAGE_BYTES", 10*1024*1024),
		WorkerCount:     envInt("MTA_WORKER_COUNT", 4),
		ScanInterval:    envDuration("MTA_SCAN_INTERVAL", 5*time.Second),
		DialTimeout:     envDuration("MTA_DIAL_TIMEOUT", 8*time.Second),
		SendTimeout:     envDuration("MTA_SEND_TIMEOUT", 20*time.Second),
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
