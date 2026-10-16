package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// applyEnvConfig overlays environment values on the defaults and YAML configuration.
func applyEnvConfig(cfg Config) Config {
	cfg.QueueMaxMessages = envInt64("MTA_QUEUE_MAX_MESSAGES", cfg.QueueMaxMessages)
	cfg.QueueMaxBytes = envInt64("MTA_QUEUE_MAX_BYTES", cfg.QueueMaxBytes)
	cfg.QueueLowWatermarkPercent = envInt64("MTA_QUEUE_LOW_WATERMARK_PERCENT", cfg.QueueLowWatermarkPercent)
	cfg.QueueMinFreeBytes = envInt64("MTA_QUEUE_MIN_FREE_BYTES", cfg.QueueMinFreeBytes)

	cfg.ListenAddr = env("MTA_LISTEN_ADDR", cfg.ListenAddr)
	cfg.SubmissionAddr = env("MTA_SUBMISSION_ADDR", cfg.SubmissionAddr)
	cfg.SubmissionAuth = envBool("MTA_SUBMISSION_AUTH_REQUIRED", cfg.SubmissionAuth)
	cfg.SubmissionUsers = envOrFile("MTA_SUBMISSION_USERS", "MTA_SUBMISSION_USERS_FILE", cfg.SubmissionUsers)
	cfg.SubmissionAuthBackend = envEnum("MTA_SUBMISSION_AUTH_BACKEND", cfg.SubmissionAuthBackend, []string{"static", "sqlite"})
	cfg.SubmissionAuthDSN = env("MTA_SUBMISSION_AUTH_DSN", cfg.SubmissionAuthDSN)
	cfg.SubmissionBearerIntrospectionURL = env("MTA_SUBMISSION_BEARER_INTROSPECTION_URL", cfg.SubmissionBearerIntrospectionURL)
	cfg.SubmissionBearerClientID = env("MTA_SUBMISSION_BEARER_CLIENT_ID", cfg.SubmissionBearerClientID)
	cfg.SubmissionBearerClientSecret = envOrFile("MTA_SUBMISSION_BEARER_CLIENT_SECRET", "MTA_SUBMISSION_BEARER_CLIENT_SECRET_FILE", cfg.SubmissionBearerClientSecret)
	cfg.SubmissionBearerIssuer = env("MTA_SUBMISSION_BEARER_ISSUER", cfg.SubmissionBearerIssuer)
	cfg.SubmissionBearerAudience = env("MTA_SUBMISSION_BEARER_AUDIENCE", cfg.SubmissionBearerAudience)
	cfg.SubmissionBearerUsernameClaim = env("MTA_SUBMISSION_BEARER_USERNAME_CLAIM", cfg.SubmissionBearerUsernameClaim)
	cfg.SubmissionBearerRequiredScope = env("MTA_SUBMISSION_BEARER_REQUIRED_SCOPE", cfg.SubmissionBearerRequiredScope)

	cfg.SubmissionSenderID = envBool("MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY", cfg.SubmissionSenderID)
	cfg.LogLevel = env("MTA_LOG_LEVEL", cfg.LogLevel)
	cfg.ObservabilityAddr = env("MTA_OBSERVABILITY_ADDR", cfg.ObservabilityAddr)
	cfg.OTELEnabled = envBool("MTA_OTEL_ENABLED", cfg.OTELEnabled)
	cfg.OTELServiceName = env("MTA_OTEL_SERVICE_NAME", cfg.OTELServiceName)
	cfg.OTELServiceVersion = env("MTA_OTEL_SERVICE_VERSION", cfg.OTELServiceVersion)
	cfg.OTELExporterOTLPEndpoint = env("MTA_OTEL_EXPORTER_OTLP_ENDPOINT", cfg.OTELExporterOTLPEndpoint)
	cfg.OTELExporterOTLPInsecure = envBool("MTA_OTEL_EXPORTER_OTLP_INSECURE", cfg.OTELExporterOTLPInsecure)
	cfg.OTELTraceSampleRatio = normalizeSampleRatio(envFloat64("MTA_OTEL_TRACE_SAMPLE_RATIO", cfg.OTELTraceSampleRatio), cfg.OTELTraceSampleRatio)
	cfg.AdminAddr = env("MTA_ADMIN_ADDR", cfg.AdminAddr)
	cfg.AdminTokens = envOrFile("MTA_ADMIN_TOKENS", "MTA_ADMIN_TOKENS_FILE", cfg.AdminTokens)
	cfg.AdminAuthBackend = envEnum("MTA_ADMIN_AUTH_BACKEND", cfg.AdminAuthBackend, []string{"config", "sqlite"})
	cfg.AdminAuthDSN = env("MTA_ADMIN_AUTH_DSN", cfg.AdminAuthDSN)
	cfg.Hostname = env("MTA_HOSTNAME", cfg.Hostname)
	cfg.QueueDir = env("MTA_QUEUE_DIR", cfg.QueueDir)
	cfg.QueueBackend = env("MTA_QUEUE_BACKEND", cfg.QueueBackend)
	cfg.KafkaBrokers = envCSV("MTA_KAFKA_BROKERS", cfg.KafkaBrokers)
	cfg.KafkaConsumerGroup = env("MTA_KAFKA_CONSUMER_GROUP", cfg.KafkaConsumerGroup)
	cfg.KafkaTopicInbound = env("MTA_KAFKA_TOPIC_INBOUND", cfg.KafkaTopicInbound)
	cfg.KafkaTopicRetry = env("MTA_KAFKA_TOPIC_RETRY", cfg.KafkaTopicRetry)
	cfg.KafkaTopicDLQ = env("MTA_KAFKA_TOPIC_DLQ", cfg.KafkaTopicDLQ)
	cfg.KafkaTopicSent = env("MTA_KAFKA_TOPIC_SENT", cfg.KafkaTopicSent)
	cfg.TLSCertFile = env("MTA_TLS_CERT_FILE", cfg.TLSCertFile)
	cfg.TLSKeyFile = env("MTA_TLS_KEY_FILE", cfg.TLSKeyFile)
	cfg.IngressRateLimit = envInt("MTA_INGRESS_RATE_LIMIT_PER_MINUTE", cfg.IngressRateLimit)
	cfg.RateLimitRules = env("MTA_RATE_LIMIT_RULES", cfg.RateLimitRules)
	cfg.RateLimitBackend = envEnum("MTA_RATE_LIMIT_BACKEND", cfg.RateLimitBackend, []string{"memory", "redis"})
	cfg.RateLimitRedisAddrs = envCSV("MTA_RATE_LIMIT_REDIS_ADDRS", cfg.RateLimitRedisAddrs)
	cfg.RateLimitRedisUsername = env("MTA_RATE_LIMIT_REDIS_USERNAME", cfg.RateLimitRedisUsername)
	cfg.RateLimitRedisPassword = env("MTA_RATE_LIMIT_REDIS_PASSWORD", cfg.RateLimitRedisPassword)
	cfg.RateLimitRedisDB = envInt("MTA_RATE_LIMIT_REDIS_DB", cfg.RateLimitRedisDB)
	cfg.RateLimitRedisKeyPrefix = env("MTA_RATE_LIMIT_REDIS_KEY_PREFIX", cfg.RateLimitRedisKeyPrefix)
	cfg.DNSBLZones = envCSV("MTA_DNSBL_ZONES", cfg.DNSBLZones)
	cfg.DNSBLCacheTTL = envDuration("MTA_DNSBL_CACHE_TTL", cfg.DNSBLCacheTTL)
	cfg.DANEDNSSECTrustModel = envEnum("MTA_DANE_DNSSEC_TRUST_MODEL", cfg.DANEDNSSECTrustModel, []string{"ad_required", "insecure_allow_unsigned"})
	cfg.MTASTSCacheTTL = envDuration("MTA_MTA_STS_CACHE_TTL", cfg.MTASTSCacheTTL)
	cfg.MTASTSFetchTimeout = envDuration("MTA_MTA_STS_FETCH_TIMEOUT", cfg.MTASTSFetchTimeout)
	cfg.DeliveryMode = env("MTA_DELIVERY_MODE", cfg.DeliveryMode)
	cfg.SpoolBackend = envEnum("MTA_SPOOL_BACKEND", cfg.SpoolBackend, []string{"local", "s3"})
	cfg.LocalSpoolDir = env("MTA_LOCAL_SPOOL_DIR", cfg.LocalSpoolDir)
	cfg.SpoolS3Bucket = env("MTA_SPOOL_S3_BUCKET", cfg.SpoolS3Bucket)
	cfg.SpoolS3Prefix = env("MTA_SPOOL_S3_PREFIX", cfg.SpoolS3Prefix)
	cfg.SpoolS3Endpoint = env("MTA_SPOOL_S3_ENDPOINT", cfg.SpoolS3Endpoint)
	cfg.SpoolS3Region = env("MTA_SPOOL_S3_REGION", cfg.SpoolS3Region)
	cfg.SpoolS3AccessKey = env("MTA_SPOOL_S3_ACCESS_KEY", cfg.SpoolS3AccessKey)
	cfg.SpoolS3SecretKey = env("MTA_SPOOL_S3_SECRET_KEY", cfg.SpoolS3SecretKey)
	cfg.SpoolS3ForcePathStyle = envBool("MTA_SPOOL_S3_FORCE_PATH_STYLE", cfg.SpoolS3ForcePathStyle)
	cfg.SpoolS3UseTLS = envBool("MTA_SPOOL_S3_USE_TLS", cfg.SpoolS3UseTLS)
	cfg.RelayHost = env("MTA_RELAY_HOST", cfg.RelayHost)
	cfg.RelayPort = envInt("MTA_RELAY_PORT", cfg.RelayPort)
	cfg.RelayRequireTLS = envBool("MTA_RELAY_REQUIRE_TLS", cfg.RelayRequireTLS)
	cfg.MaxMessageBytes = envInt64("MTA_MAX_MESSAGE_BYTES", cfg.MaxMessageBytes)
	cfg.WorkerCount = envInt("MTA_WORKER_COUNT", cfg.WorkerCount)
	cfg.MaxAttempts = envInt("MTA_MAX_ATTEMPTS", cfg.MaxAttempts)
	cfg.MaxRetryAge = envDuration("MTA_MAX_RETRY_AGE", cfg.MaxRetryAge)
	cfg.RetrySchedule = envDurationList("MTA_RETRY_SCHEDULE", cfg.RetrySchedule)
	cfg.ScanInterval = envDuration("MTA_SCAN_INTERVAL", cfg.ScanInterval)
	cfg.DialTimeout = envDuration("MTA_DIAL_TIMEOUT", cfg.DialTimeout)
	cfg.SendTimeout = envDuration("MTA_SEND_TIMEOUT", cfg.SendTimeout)
	cfg.DKIMSignDomain = env("MTA_DKIM_SIGN_DOMAIN", cfg.DKIMSignDomain)
	cfg.DKIMSignSelector = env("MTA_DKIM_SIGN_SELECTOR", cfg.DKIMSignSelector)
	cfg.DKIMPrivateKeyFile = env("MTA_DKIM_PRIVATE_KEY_FILE", cfg.DKIMPrivateKeyFile)
	cfg.DKIMSignHeaders = env("MTA_DKIM_SIGN_HEADERS", cfg.DKIMSignHeaders)
	cfg.ARCFailurePolicy = envEnum("MTA_ARC_FAILURE_POLICY", cfg.ARCFailurePolicy, []string{"accept", "quarantine", "reject"})
	cfg.SPFHeloPolicy = envEnum("MTA_SPF_HELO_POLICY", cfg.SPFHeloPolicy, []string{"off", "advisory", "enforce"})
	cfg.SPFMailFromPolicy = envEnum("MTA_SPF_MAILFROM_POLICY", cfg.SPFMailFromPolicy, []string{"off", "advisory", "enforce"})
	cfg.DomainMaxConcurrentDefault = envInt("MTA_DOMAIN_MAX_CONCURRENT_DEFAULT", cfg.DomainMaxConcurrentDefault)
	cfg.DomainMaxConcurrentRules = env("MTA_DOMAIN_MAX_CONCURRENT_RULES", cfg.DomainMaxConcurrentRules)
	cfg.DomainThrottleBackend = envEnum("MTA_DOMAIN_THROTTLE_BACKEND", cfg.DomainThrottleBackend, []string{"memory", "redis"})
	cfg.DomainThrottleRedisAddrs = envCSV("MTA_DOMAIN_THROTTLE_REDIS_ADDRS", cfg.DomainThrottleRedisAddrs)
	cfg.DomainThrottleRedisUsername = env("MTA_DOMAIN_THROTTLE_REDIS_USERNAME", cfg.DomainThrottleRedisUsername)
	cfg.DomainThrottleRedisPassword = env("MTA_DOMAIN_THROTTLE_REDIS_PASSWORD", cfg.DomainThrottleRedisPassword)
	cfg.DomainThrottleRedisDB = envInt("MTA_DOMAIN_THROTTLE_REDIS_DB", cfg.DomainThrottleRedisDB)
	cfg.DomainThrottleRedisKeyPrefix = env("MTA_DOMAIN_THROTTLE_REDIS_KEY_PREFIX", cfg.DomainThrottleRedisKeyPrefix)
	cfg.DomainAdaptiveThrottle = envBool("MTA_DOMAIN_ADAPTIVE_THROTTLE", cfg.DomainAdaptiveThrottle)
	cfg.DomainTempFailThreshold = envFloat64("MTA_DOMAIN_TEMPFAIL_THRESHOLD", cfg.DomainTempFailThreshold)
	cfg.DomainPenaltyMax = envDuration("MTA_DOMAIN_PENALTY_MAX", cfg.DomainPenaltyMax)
	cfg.DataRetentionSent = envDuration("MTA_DATA_RETENTION_SENT", cfg.DataRetentionSent)
	cfg.DataRetentionDLQ = envDuration("MTA_DATA_RETENTION_DLQ", cfg.DataRetentionDLQ)
	cfg.DataRetentionPoison = envDuration("MTA_DATA_RETENTION_POISON", cfg.DataRetentionPoison)
	cfg.RetentionSweepInterval = envDuration("MTA_RETENTION_SWEEP_INTERVAL", cfg.RetentionSweepInterval)
	cfg.ReputationStartDate = env("MTA_REPUTATION_START_DATE", cfg.ReputationStartDate)
	cfg.ReputationWarmupRules = env("MTA_REPUTATION_WARMUP_RULES", cfg.ReputationWarmupRules)
	cfg.ReputationBounceThreshold = envFloat64("MTA_REPUTATION_BOUNCE_THRESHOLD", cfg.ReputationBounceThreshold)
	cfg.ReputationComplaintThresh = envFloat64("MTA_REPUTATION_COMPLAINT_THRESHOLD", cfg.ReputationComplaintThresh)
	cfg.ReputationMinSamples = envInt("MTA_REPUTATION_MIN_SAMPLES", cfg.ReputationMinSamples)

	return cfg
}

func envEnum(k, def string, allowed []string) string {
	return normalizeEnum(os.Getenv(k), def, allowed)
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
