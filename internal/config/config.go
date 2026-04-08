package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
	RateLimitBackend           string
	RateLimitRedisAddrs        []string
	RateLimitRedisUsername     string
	RateLimitRedisPassword     string
	RateLimitRedisDB           int
	RateLimitRedisKeyPrefix    string
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

type yamlConfig struct {
	ListenAddr                 *string  `yaml:"listen_addr"`
	SubmissionAddr             *string  `yaml:"submission_addr"`
	SubmissionAuth             *bool    `yaml:"submission_auth_required"`
	SubmissionUsers            *string  `yaml:"submission_users"`
	SubmissionSenderID         *bool    `yaml:"submission_enforce_sender_identity"`
	LogLevel                   *string  `yaml:"log_level"`
	ObservabilityAddr          *string  `yaml:"observability_addr"`
	AdminAddr                  *string  `yaml:"admin_addr"`
	AdminTokens                *string  `yaml:"admin_tokens"`
	Hostname                   *string  `yaml:"hostname"`
	QueueDir                   *string  `yaml:"queue_dir"`
	QueueBackend               *string  `yaml:"queue_backend"`
	KafkaBrokers               []string `yaml:"kafka_brokers"`
	KafkaConsumerGroup         *string  `yaml:"kafka_consumer_group"`
	KafkaTopicInbound          *string  `yaml:"kafka_topic_inbound"`
	KafkaTopicRetry            *string  `yaml:"kafka_topic_retry"`
	KafkaTopicDLQ              *string  `yaml:"kafka_topic_dlq"`
	KafkaTopicSent             *string  `yaml:"kafka_topic_sent"`
	TLSCertFile                *string  `yaml:"tls_cert_file"`
	TLSKeyFile                 *string  `yaml:"tls_key_file"`
	IngressRateLimit           *int     `yaml:"ingress_rate_limit_per_minute"`
	RateLimitRules             *string  `yaml:"rate_limit_rules"`
	RateLimitBackend           *string  `yaml:"rate_limit_backend"`
	RateLimitRedisAddrs        []string `yaml:"rate_limit_redis_addrs"`
	RateLimitRedisUsername     *string  `yaml:"rate_limit_redis_username"`
	RateLimitRedisPassword     *string  `yaml:"rate_limit_redis_password"`
	RateLimitRedisDB           *int     `yaml:"rate_limit_redis_db"`
	RateLimitRedisKeyPrefix    *string  `yaml:"rate_limit_redis_key_prefix"`
	DNSBLZones                 []string `yaml:"dnsbl_zones"`
	DNSBLCacheTTL              *string  `yaml:"dnsbl_cache_ttl"`
	DANEDNSSECTrustModel       *string  `yaml:"dane_dnssec_trust_model"`
	MTASTSCacheTTL             *string  `yaml:"mta_sts_cache_ttl"`
	MTASTSFetchTimeout         *string  `yaml:"mta_sts_fetch_timeout"`
	DeliveryMode               *string  `yaml:"delivery_mode"`
	LocalSpoolDir              *string  `yaml:"local_spool_dir"`
	RelayHost                  *string  `yaml:"relay_host"`
	RelayPort                  *int     `yaml:"relay_port"`
	RelayRequireTLS            *bool    `yaml:"relay_require_tls"`
	MaxMessageBytes            *int64   `yaml:"max_message_bytes"`
	WorkerCount                *int     `yaml:"worker_count"`
	MaxAttempts                *int     `yaml:"max_attempts"`
	MaxRetryAge                *string  `yaml:"max_retry_age"`
	RetrySchedule              []string `yaml:"retry_schedule"`
	ScanInterval               *string  `yaml:"scan_interval"`
	DialTimeout                *string  `yaml:"dial_timeout"`
	SendTimeout                *string  `yaml:"send_timeout"`
	DKIMSignDomain             *string  `yaml:"dkim_sign_domain"`
	DKIMSignSelector           *string  `yaml:"dkim_sign_selector"`
	DKIMPrivateKeyFile         *string  `yaml:"dkim_private_key_file"`
	DKIMSignHeaders            *string  `yaml:"dkim_sign_headers"`
	ARCFailurePolicy           *string  `yaml:"arc_failure_policy"`
	SPFHeloPolicy              *string  `yaml:"spf_helo_policy"`
	SPFMailFromPolicy          *string  `yaml:"spf_mailfrom_policy"`
	DomainMaxConcurrentDefault *int     `yaml:"domain_max_concurrent_default"`
	DomainMaxConcurrentRules   *string  `yaml:"domain_max_concurrent_rules"`
	DomainAdaptiveThrottle     *bool    `yaml:"domain_adaptive_throttle"`
	DomainTempFailThreshold    *float64 `yaml:"domain_tempfail_threshold"`
	DomainPenaltyMax           *string  `yaml:"domain_penalty_max"`
	DataRetentionSent          *string  `yaml:"data_retention_sent"`
	DataRetentionDLQ           *string  `yaml:"data_retention_dlq"`
	DataRetentionPoison        *string  `yaml:"data_retention_poison"`
	RetentionSweepInterval     *string  `yaml:"retention_sweep_interval"`
	ReputationStartDate        *string  `yaml:"reputation_start_date"`
	ReputationWarmupRules      *string  `yaml:"reputation_warmup_rules"`
	ReputationBounceThreshold  *float64 `yaml:"reputation_bounce_threshold"`
	ReputationComplaintThresh  *float64 `yaml:"reputation_complaint_threshold"`
	ReputationMinSamples       *int     `yaml:"reputation_min_samples"`
}

func Load() (Config, error) {
	return LoadWithPath("")
}

func LoadWithPath(explicitPath string) (Config, error) {
	cfg := defaultConfig()

	configPath, requestedPath, explicit := resolveConfigPath(explicitPath)
	if configPath != "" {
		loaded, err := loadYAMLConfig(configPath, cfg)
		if err != nil {
			return Config{}, err
		}
		cfg = loaded
	} else if explicit {
		return Config{}, fmt.Errorf("config file %q not found", requestedPath)
	}

	cfg.ListenAddr = env("MTA_LISTEN_ADDR", cfg.ListenAddr)
	cfg.SubmissionAddr = env("MTA_SUBMISSION_ADDR", cfg.SubmissionAddr)
	cfg.SubmissionAuth = envBool("MTA_SUBMISSION_AUTH_REQUIRED", cfg.SubmissionAuth)
	cfg.SubmissionUsers = envOrFile("MTA_SUBMISSION_USERS", "MTA_SUBMISSION_USERS_FILE", cfg.SubmissionUsers)
	cfg.SubmissionSenderID = envBool("MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY", cfg.SubmissionSenderID)
	cfg.LogLevel = env("MTA_LOG_LEVEL", cfg.LogLevel)
	cfg.ObservabilityAddr = env("MTA_OBSERVABILITY_ADDR", cfg.ObservabilityAddr)
	cfg.AdminAddr = env("MTA_ADMIN_ADDR", cfg.AdminAddr)
	cfg.AdminTokens = envOrFile("MTA_ADMIN_TOKENS", "MTA_ADMIN_TOKENS_FILE", cfg.AdminTokens)
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
	cfg.LocalSpoolDir = env("MTA_LOCAL_SPOOL_DIR", cfg.LocalSpoolDir)
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

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		ListenAddr:                 ":2525",
		SubmissionAddr:             "",
		SubmissionAuth:             true,
		SubmissionUsers:            "",
		SubmissionSenderID:         true,
		LogLevel:                   "info",
		ObservabilityAddr:          ":9090",
		AdminAddr:                  "",
		AdminTokens:                "",
		Hostname:                   "kuroshio.local",
		QueueDir:                   "./var/queue",
		QueueBackend:               "local",
		KafkaBrokers:               []string{"localhost:9092"},
		KafkaConsumerGroup:         "kuroshio-mta",
		KafkaTopicInbound:          "mail.inbound",
		KafkaTopicRetry:            "mail.retry",
		KafkaTopicDLQ:              "mail.dlq",
		KafkaTopicSent:             "mail.sent",
		TLSCertFile:                "",
		TLSKeyFile:                 "",
		IngressRateLimit:           100,
		RateLimitRules:             "",
		RateLimitBackend:           "memory",
		RateLimitRedisAddrs:        []string{"localhost:6379"},
		RateLimitRedisUsername:     "",
		RateLimitRedisPassword:     "",
		RateLimitRedisDB:           0,
		RateLimitRedisKeyPrefix:    "kuroshio:ratelimit",
		DNSBLZones:                 []string{},
		DNSBLCacheTTL:              5 * time.Minute,
		DANEDNSSECTrustModel:       "ad_required",
		MTASTSCacheTTL:             time.Hour,
		MTASTSFetchTimeout:         5 * time.Second,
		DeliveryMode:               "mx",
		LocalSpoolDir:              "./var/spool",
		RelayHost:                  "",
		RelayPort:                  25,
		RelayRequireTLS:            false,
		MaxMessageBytes:            10 * 1024 * 1024,
		WorkerCount:                4,
		MaxAttempts:                12,
		MaxRetryAge:                5 * 24 * time.Hour,
		RetrySchedule:              []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour},
		ScanInterval:               5 * time.Second,
		DialTimeout:                8 * time.Second,
		SendTimeout:                20 * time.Second,
		DKIMSignDomain:             "",
		DKIMSignSelector:           "",
		DKIMPrivateKeyFile:         "",
		DKIMSignHeaders:            "from:to:subject:date:message-id",
		ARCFailurePolicy:           "accept",
		SPFHeloPolicy:              "advisory",
		SPFMailFromPolicy:          "advisory",
		DomainMaxConcurrentDefault: 8,
		DomainMaxConcurrentRules:   "",
		DomainAdaptiveThrottle:     true,
		DomainTempFailThreshold:    0.3,
		DomainPenaltyMax:           5 * time.Second,
		DataRetentionSent:          30 * 24 * time.Hour,
		DataRetentionDLQ:           90 * 24 * time.Hour,
		DataRetentionPoison:        180 * 24 * time.Hour,
		RetentionSweepInterval:     time.Hour,
		ReputationStartDate:        "",
		ReputationWarmupRules:      "0:100,7:1000,14:5000",
		ReputationBounceThreshold:  0.05,
		ReputationComplaintThresh:  0.001,
		ReputationMinSamples:       100,
	}
}

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

func loadYAMLConfig(path string, base Config) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	var raw yamlConfig
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config file %q: %w", path, err)
	}

	cfg := base
	if raw.ListenAddr != nil {
		cfg.ListenAddr = *raw.ListenAddr
	}
	if raw.SubmissionAddr != nil {
		cfg.SubmissionAddr = *raw.SubmissionAddr
	}
	if raw.SubmissionAuth != nil {
		cfg.SubmissionAuth = *raw.SubmissionAuth
	}
	if raw.SubmissionUsers != nil {
		cfg.SubmissionUsers = *raw.SubmissionUsers
	}
	if raw.SubmissionSenderID != nil {
		cfg.SubmissionSenderID = *raw.SubmissionSenderID
	}
	if raw.LogLevel != nil {
		cfg.LogLevel = *raw.LogLevel
	}
	if raw.ObservabilityAddr != nil {
		cfg.ObservabilityAddr = *raw.ObservabilityAddr
	}
	if raw.AdminAddr != nil {
		cfg.AdminAddr = *raw.AdminAddr
	}
	if raw.AdminTokens != nil {
		cfg.AdminTokens = *raw.AdminTokens
	}
	if raw.Hostname != nil {
		cfg.Hostname = *raw.Hostname
	}
	if raw.QueueDir != nil {
		cfg.QueueDir = *raw.QueueDir
	}
	if raw.QueueBackend != nil {
		cfg.QueueBackend = *raw.QueueBackend
	}
	if raw.KafkaBrokers != nil {
		cfg.KafkaBrokers = append([]string(nil), raw.KafkaBrokers...)
	}
	if raw.KafkaConsumerGroup != nil {
		cfg.KafkaConsumerGroup = *raw.KafkaConsumerGroup
	}
	if raw.KafkaTopicInbound != nil {
		cfg.KafkaTopicInbound = *raw.KafkaTopicInbound
	}
	if raw.KafkaTopicRetry != nil {
		cfg.KafkaTopicRetry = *raw.KafkaTopicRetry
	}
	if raw.KafkaTopicDLQ != nil {
		cfg.KafkaTopicDLQ = *raw.KafkaTopicDLQ
	}
	if raw.KafkaTopicSent != nil {
		cfg.KafkaTopicSent = *raw.KafkaTopicSent
	}
	if raw.TLSCertFile != nil {
		cfg.TLSCertFile = *raw.TLSCertFile
	}
	if raw.TLSKeyFile != nil {
		cfg.TLSKeyFile = *raw.TLSKeyFile
	}
	if raw.IngressRateLimit != nil {
		cfg.IngressRateLimit = *raw.IngressRateLimit
	}
	if raw.RateLimitRules != nil {
		cfg.RateLimitRules = *raw.RateLimitRules
	}
	if raw.RateLimitBackend != nil {
		cfg.RateLimitBackend = normalizeEnum(*raw.RateLimitBackend, cfg.RateLimitBackend, []string{"memory", "redis"})
	}
	if raw.RateLimitRedisAddrs != nil {
		cfg.RateLimitRedisAddrs = append([]string(nil), raw.RateLimitRedisAddrs...)
	}
	if raw.RateLimitRedisUsername != nil {
		cfg.RateLimitRedisUsername = *raw.RateLimitRedisUsername
	}
	if raw.RateLimitRedisPassword != nil {
		cfg.RateLimitRedisPassword = *raw.RateLimitRedisPassword
	}
	if raw.RateLimitRedisDB != nil {
		cfg.RateLimitRedisDB = *raw.RateLimitRedisDB
	}
	if raw.RateLimitRedisKeyPrefix != nil {
		cfg.RateLimitRedisKeyPrefix = *raw.RateLimitRedisKeyPrefix
	}
	if raw.DNSBLZones != nil {
		cfg.DNSBLZones = append([]string(nil), raw.DNSBLZones...)
	}
	if raw.DNSBLCacheTTL != nil {
		cfg.DNSBLCacheTTL, err = parseYAMLDuration(*raw.DNSBLCacheTTL, "dnsbl_cache_ttl")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DANEDNSSECTrustModel != nil {
		cfg.DANEDNSSECTrustModel = normalizeEnum(*raw.DANEDNSSECTrustModel, cfg.DANEDNSSECTrustModel, []string{"ad_required", "insecure_allow_unsigned"})
	}
	if raw.MTASTSCacheTTL != nil {
		cfg.MTASTSCacheTTL, err = parseYAMLDuration(*raw.MTASTSCacheTTL, "mta_sts_cache_ttl")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.MTASTSFetchTimeout != nil {
		cfg.MTASTSFetchTimeout, err = parseYAMLDuration(*raw.MTASTSFetchTimeout, "mta_sts_fetch_timeout")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DeliveryMode != nil {
		cfg.DeliveryMode = *raw.DeliveryMode
	}
	if raw.LocalSpoolDir != nil {
		cfg.LocalSpoolDir = *raw.LocalSpoolDir
	}
	if raw.RelayHost != nil {
		cfg.RelayHost = *raw.RelayHost
	}
	if raw.RelayPort != nil {
		cfg.RelayPort = *raw.RelayPort
	}
	if raw.RelayRequireTLS != nil {
		cfg.RelayRequireTLS = *raw.RelayRequireTLS
	}
	if raw.MaxMessageBytes != nil {
		cfg.MaxMessageBytes = *raw.MaxMessageBytes
	}
	if raw.WorkerCount != nil {
		cfg.WorkerCount = *raw.WorkerCount
	}
	if raw.MaxAttempts != nil {
		cfg.MaxAttempts = *raw.MaxAttempts
	}
	if raw.MaxRetryAge != nil {
		cfg.MaxRetryAge, err = parseYAMLDuration(*raw.MaxRetryAge, "max_retry_age")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.RetrySchedule != nil {
		cfg.RetrySchedule, err = parseYAMLDurations(raw.RetrySchedule, "retry_schedule")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.ScanInterval != nil {
		cfg.ScanInterval, err = parseYAMLDuration(*raw.ScanInterval, "scan_interval")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DialTimeout != nil {
		cfg.DialTimeout, err = parseYAMLDuration(*raw.DialTimeout, "dial_timeout")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.SendTimeout != nil {
		cfg.SendTimeout, err = parseYAMLDuration(*raw.SendTimeout, "send_timeout")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DKIMSignDomain != nil {
		cfg.DKIMSignDomain = *raw.DKIMSignDomain
	}
	if raw.DKIMSignSelector != nil {
		cfg.DKIMSignSelector = *raw.DKIMSignSelector
	}
	if raw.DKIMPrivateKeyFile != nil {
		cfg.DKIMPrivateKeyFile = *raw.DKIMPrivateKeyFile
	}
	if raw.DKIMSignHeaders != nil {
		cfg.DKIMSignHeaders = *raw.DKIMSignHeaders
	}
	if raw.ARCFailurePolicy != nil {
		cfg.ARCFailurePolicy = normalizeEnum(*raw.ARCFailurePolicy, cfg.ARCFailurePolicy, []string{"accept", "quarantine", "reject"})
	}
	if raw.SPFHeloPolicy != nil {
		cfg.SPFHeloPolicy = normalizeEnum(*raw.SPFHeloPolicy, cfg.SPFHeloPolicy, []string{"off", "advisory", "enforce"})
	}
	if raw.SPFMailFromPolicy != nil {
		cfg.SPFMailFromPolicy = normalizeEnum(*raw.SPFMailFromPolicy, cfg.SPFMailFromPolicy, []string{"off", "advisory", "enforce"})
	}
	if raw.DomainMaxConcurrentDefault != nil {
		cfg.DomainMaxConcurrentDefault = *raw.DomainMaxConcurrentDefault
	}
	if raw.DomainMaxConcurrentRules != nil {
		cfg.DomainMaxConcurrentRules = *raw.DomainMaxConcurrentRules
	}
	if raw.DomainAdaptiveThrottle != nil {
		cfg.DomainAdaptiveThrottle = *raw.DomainAdaptiveThrottle
	}
	if raw.DomainTempFailThreshold != nil {
		cfg.DomainTempFailThreshold = *raw.DomainTempFailThreshold
	}
	if raw.DomainPenaltyMax != nil {
		cfg.DomainPenaltyMax, err = parseYAMLDuration(*raw.DomainPenaltyMax, "domain_penalty_max")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DataRetentionSent != nil {
		cfg.DataRetentionSent, err = parseYAMLDuration(*raw.DataRetentionSent, "data_retention_sent")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DataRetentionDLQ != nil {
		cfg.DataRetentionDLQ, err = parseYAMLDuration(*raw.DataRetentionDLQ, "data_retention_dlq")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.DataRetentionPoison != nil {
		cfg.DataRetentionPoison, err = parseYAMLDuration(*raw.DataRetentionPoison, "data_retention_poison")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.RetentionSweepInterval != nil {
		cfg.RetentionSweepInterval, err = parseYAMLDuration(*raw.RetentionSweepInterval, "retention_sweep_interval")
		if err != nil {
			return Config{}, err
		}
	}
	if raw.ReputationStartDate != nil {
		cfg.ReputationStartDate = *raw.ReputationStartDate
	}
	if raw.ReputationWarmupRules != nil {
		cfg.ReputationWarmupRules = *raw.ReputationWarmupRules
	}
	if raw.ReputationBounceThreshold != nil {
		cfg.ReputationBounceThreshold = *raw.ReputationBounceThreshold
	}
	if raw.ReputationComplaintThresh != nil {
		cfg.ReputationComplaintThresh = *raw.ReputationComplaintThresh
	}
	if raw.ReputationMinSamples != nil {
		cfg.ReputationMinSamples = *raw.ReputationMinSamples
	}

	return cfg, nil
}

func parseYAMLDuration(v, field string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", field, v, err)
	}
	return d, nil
}

func parseYAMLDurations(values []string, field string) ([]time.Duration, error) {
	out := make([]time.Duration, 0, len(values))
	for _, value := range values {
		d, err := parseYAMLDuration(value, field)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

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
