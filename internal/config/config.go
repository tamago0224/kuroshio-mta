package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr                 string
	SubmissionAddr             string
	SubmissionAuth             bool
	SubmissionUsers            string
	SubmissionSenderID         bool
	LogLevel                   string
	ObservabilityAddr          string
	AdminAddr                  string
	AdminTokens                string
	Hostname                   string
	QueueDir                   string
	QueueBackend               string
	KafkaBrokers               []string
	KafkaConsumerGroup         string
	KafkaTopicInbound          string
	KafkaTopicRetry            string
	KafkaTopicDLQ              string
	KafkaTopicSent             string
	TLSCertFile                string
	TLSKeyFile                 string
	IngressRateLimit           int
	RateLimitRules             string
	DNSBLZones                 []string
	DNSBLCacheTTL              time.Duration
	DANEDNSSECTrustModel       string
	MTASTSCacheTTL             time.Duration
	MTASTSFetchTimeout         time.Duration
	DeliveryMode               string
	LocalSpoolDir              string
	RelayHost                  string
	RelayPort                  int
	RelayRequireTLS            bool
	MaxMessageBytes            int64
	WorkerCount                int
	MaxAttempts                int
	MaxRetryAge                time.Duration
	RetrySchedule              []time.Duration
	ScanInterval               time.Duration
	DialTimeout                time.Duration
	SendTimeout                time.Duration
	DKIMSignDomain             string
	DKIMSignSelector           string
	DKIMPrivateKeyFile         string
	DKIMSignHeaders            string
	ARCFailurePolicy           string
	SPFHeloPolicy              string
	SPFMailFromPolicy          string
	DomainMaxConcurrentDefault int
	DomainMaxConcurrentRules   string
	DomainAdaptiveThrottle     bool
	DomainTempFailThreshold    float64
	DomainPenaltyMax           time.Duration
	DataRetentionSent          time.Duration
	DataRetentionDLQ           time.Duration
	DataRetentionPoison        time.Duration
	RetentionSweepInterval     time.Duration
	ReputationStartDate        string
	ReputationWarmupRules      string
	ReputationBounceThreshold  float64
	ReputationComplaintThresh  float64
	ReputationMinSamples       int
}

func Load() Config {
	return Config{
		ListenAddr:         env("MTA_LISTEN_ADDR", ":2525"),
		SubmissionAddr:     env("MTA_SUBMISSION_ADDR", ""),
		SubmissionAuth:     envBool("MTA_SUBMISSION_AUTH_REQUIRED", true),
		SubmissionUsers:    envOrFile("MTA_SUBMISSION_USERS", "MTA_SUBMISSION_USERS_FILE", ""),
		SubmissionSenderID: envBool("MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY", true),
		LogLevel:           env("MTA_LOG_LEVEL", "info"),
		ObservabilityAddr:  env("MTA_OBSERVABILITY_ADDR", ":9090"),
		AdminAddr:          env("MTA_ADMIN_ADDR", ""),
		AdminTokens:        envOrFile("MTA_ADMIN_TOKENS", "MTA_ADMIN_TOKENS_FILE", ""),
		Hostname:           env("MTA_HOSTNAME", "orinoco.local"),
		QueueDir:           env("MTA_QUEUE_DIR", "./var/queue"),
		QueueBackend:       env("MTA_QUEUE_BACKEND", "local"),
		KafkaBrokers:       envCSV("MTA_KAFKA_BROKERS", []string{"localhost:9092"}),
		KafkaConsumerGroup: env("MTA_KAFKA_CONSUMER_GROUP", "orinoco-mta"),
		KafkaTopicInbound:  env("MTA_KAFKA_TOPIC_INBOUND", "mail.inbound"),
		KafkaTopicRetry:    env("MTA_KAFKA_TOPIC_RETRY", "mail.retry"),
		KafkaTopicDLQ:      env("MTA_KAFKA_TOPIC_DLQ", "mail.dlq"),
		KafkaTopicSent:     env("MTA_KAFKA_TOPIC_SENT", "mail.sent"),
		TLSCertFile:        env("MTA_TLS_CERT_FILE", ""),
		TLSKeyFile:         env("MTA_TLS_KEY_FILE", ""),
		IngressRateLimit:   envInt("MTA_INGRESS_RATE_LIMIT_PER_MINUTE", 100),
		RateLimitRules:     env("MTA_RATE_LIMIT_RULES", ""),
		DNSBLZones:         envCSV("MTA_DNSBL_ZONES", []string{}),
		DNSBLCacheTTL:      envDuration("MTA_DNSBL_CACHE_TTL", 5*time.Minute),
		DANEDNSSECTrustModel: envEnum(
			"MTA_DANE_DNSSEC_TRUST_MODEL",
			"ad_required",
			[]string{"ad_required", "insecure_allow_unsigned"},
		),
		MTASTSCacheTTL:     envDuration("MTA_MTA_STS_CACHE_TTL", time.Hour),
		MTASTSFetchTimeout: envDuration("MTA_MTA_STS_FETCH_TIMEOUT", 5*time.Second),
		DeliveryMode:       env("MTA_DELIVERY_MODE", "mx"),
		LocalSpoolDir:      env("MTA_LOCAL_SPOOL_DIR", "./var/spool"),
		RelayHost:          env("MTA_RELAY_HOST", ""),
		RelayPort:          envInt("MTA_RELAY_PORT", 25),
		RelayRequireTLS:    envBool("MTA_RELAY_REQUIRE_TLS", false),
		MaxMessageBytes:    envInt64("MTA_MAX_MESSAGE_BYTES", 10*1024*1024),
		WorkerCount:        envInt("MTA_WORKER_COUNT", 4),
		MaxAttempts:        envInt("MTA_MAX_ATTEMPTS", 12),
		MaxRetryAge:        envDuration("MTA_MAX_RETRY_AGE", 5*24*time.Hour),
		RetrySchedule: envDurationList(
			"MTA_RETRY_SCHEDULE",
			[]time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour},
		),
		ScanInterval:               envDuration("MTA_SCAN_INTERVAL", 5*time.Second),
		DialTimeout:                envDuration("MTA_DIAL_TIMEOUT", 8*time.Second),
		SendTimeout:                envDuration("MTA_SEND_TIMEOUT", 20*time.Second),
		DKIMSignDomain:             env("MTA_DKIM_SIGN_DOMAIN", ""),
		DKIMSignSelector:           env("MTA_DKIM_SIGN_SELECTOR", ""),
		DKIMPrivateKeyFile:         env("MTA_DKIM_PRIVATE_KEY_FILE", ""),
		DKIMSignHeaders:            env("MTA_DKIM_SIGN_HEADERS", "from:to:subject:date:message-id"),
		ARCFailurePolicy:           envEnum("MTA_ARC_FAILURE_POLICY", "accept", []string{"accept", "quarantine", "reject"}),
		SPFHeloPolicy:              envEnum("MTA_SPF_HELO_POLICY", "advisory", []string{"off", "advisory", "enforce"}),
		SPFMailFromPolicy:          envEnum("MTA_SPF_MAILFROM_POLICY", "advisory", []string{"off", "advisory", "enforce"}),
		DomainMaxConcurrentDefault: envInt("MTA_DOMAIN_MAX_CONCURRENT_DEFAULT", 8),
		DomainMaxConcurrentRules:   env("MTA_DOMAIN_MAX_CONCURRENT_RULES", ""),
		DomainAdaptiveThrottle:     envBool("MTA_DOMAIN_ADAPTIVE_THROTTLE", true),
		DomainTempFailThreshold:    envFloat64("MTA_DOMAIN_TEMPFAIL_THRESHOLD", 0.3),
		DomainPenaltyMax:           envDuration("MTA_DOMAIN_PENALTY_MAX", 5*time.Second),
		DataRetentionSent:          envDuration("MTA_DATA_RETENTION_SENT", 30*24*time.Hour),
		DataRetentionDLQ:           envDuration("MTA_DATA_RETENTION_DLQ", 90*24*time.Hour),
		DataRetentionPoison:        envDuration("MTA_DATA_RETENTION_POISON", 180*24*time.Hour),
		RetentionSweepInterval:     envDuration("MTA_RETENTION_SWEEP_INTERVAL", time.Hour),
		ReputationStartDate:        env("MTA_REPUTATION_START_DATE", ""),
		ReputationWarmupRules:      env("MTA_REPUTATION_WARMUP_RULES", ""),
		ReputationBounceThreshold:  envFloat64("MTA_REPUTATION_BOUNCE_THRESHOLD", 0.05),
		ReputationComplaintThresh:  envFloat64("MTA_REPUTATION_COMPLAINT_THRESHOLD", 0.001),
		ReputationMinSamples:       envInt("MTA_REPUTATION_MIN_SAMPLES", 100),
	}
}

func envEnum(k, def string, allowed []string) string {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	for _, a := range allowed {
		if v == strings.ToLower(strings.TrimSpace(a)) {
			return v
		}
	}
	return def
}

func env(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func envOrFile(k, fileK, def string) string {
	if fp := strings.TrimSpace(os.Getenv(fileK)); fp != "" {
		b, err := os.ReadFile(fp)
		if err == nil {
			s := strings.TrimSpace(string(b))
			if s != "" {
				return s
			}
		}
	}
	return env(k, def)
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

func envFloat64(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
