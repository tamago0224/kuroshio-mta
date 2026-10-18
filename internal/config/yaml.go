package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"time"
)

type yamlConfig struct {
	PolicyHook               *PolicyHookConfig `yaml:"policy_hook"`
	MXShaping                *MXShapingConfig  `yaml:"mx_shaping"`
	QueueMaxMessages         *int64            `yaml:"queue_max_messages"`
	QueueMaxBytes            *int64            `yaml:"queue_max_bytes"`
	QueueLowWatermarkPercent *int64            `yaml:"queue_low_watermark_percent"`
	QueueMinFreeBytes        *int64            `yaml:"queue_min_free_bytes"`

	ListenAddr                       *string `yaml:"listen_addr"`
	SubmissionAddr                   *string `yaml:"submission_addr"`
	SubmissionTLSAddr                *string `yaml:"submission_tls_addr"`
	SubmissionAllowInsecureAuth      *bool   `yaml:"submission_allow_insecure_auth"`
	SubmissionAuth                   *bool   `yaml:"submission_auth_required"`
	SubmissionUsers                  *string `yaml:"submission_users"`
	SubmissionAuthBackend            *string `yaml:"submission_auth_backend"`
	SubmissionAuthDSN                *string `yaml:"submission_auth_dsn"`
	SubmissionBearerIntrospectionURL *string `yaml:"submission_bearer_introspection_url"`
	SubmissionBearerClientID         *string `yaml:"submission_bearer_client_id"`
	SubmissionBearerClientSecret     *string `yaml:"submission_bearer_client_secret"`
	SubmissionBearerIssuer           *string `yaml:"submission_bearer_issuer"`
	SubmissionBearerAudience         *string `yaml:"submission_bearer_audience"`
	SubmissionBearerUsernameClaim    *string `yaml:"submission_bearer_username_claim"`
	SubmissionBearerRequiredScope    *string `yaml:"submission_bearer_required_scope"`

	SubmissionSenderID           *bool    `yaml:"submission_enforce_sender_identity"`
	LogLevel                     *string  `yaml:"log_level"`
	ObservabilityAddr            *string  `yaml:"observability_addr"`
	OTELEnabled                  *bool    `yaml:"otel_enabled"`
	OTELServiceName              *string  `yaml:"otel_service_name"`
	OTELServiceVersion           *string  `yaml:"otel_service_version"`
	OTELExporterOTLPEndpoint     *string  `yaml:"otel_exporter_otlp_endpoint"`
	OTELExporterOTLPInsecure     *bool    `yaml:"otel_exporter_otlp_insecure"`
	OTELTraceSampleRatio         *float64 `yaml:"otel_trace_sample_ratio"`
	AdminAddr                    *string  `yaml:"admin_addr"`
	AdminTokens                  *string  `yaml:"admin_tokens"`
	AdminAuthBackend             *string  `yaml:"admin_auth_backend"`
	AdminAuthDSN                 *string  `yaml:"admin_auth_dsn"`
	Hostname                     *string  `yaml:"hostname"`
	QueueDir                     *string  `yaml:"queue_dir"`
	QueueBackend                 *string  `yaml:"queue_backend"`
	KafkaBrokers                 []string `yaml:"kafka_brokers"`
	KafkaConsumerGroup           *string  `yaml:"kafka_consumer_group"`
	KafkaTopicInbound            *string  `yaml:"kafka_topic_inbound"`
	KafkaTopicRetry              *string  `yaml:"kafka_topic_retry"`
	KafkaTopicDLQ                *string  `yaml:"kafka_topic_dlq"`
	KafkaTopicSent               *string  `yaml:"kafka_topic_sent"`
	TLSCertFile                  *string  `yaml:"tls_cert_file"`
	TLSKeyFile                   *string  `yaml:"tls_key_file"`
	IngressRateLimit             *int     `yaml:"ingress_rate_limit_per_minute"`
	RateLimitRules               *string  `yaml:"rate_limit_rules"`
	RateLimitBackend             *string  `yaml:"rate_limit_backend"`
	RateLimitRedisAddrs          []string `yaml:"rate_limit_redis_addrs"`
	RateLimitRedisUsername       *string  `yaml:"rate_limit_redis_username"`
	RateLimitRedisPassword       *string  `yaml:"rate_limit_redis_password"`
	RateLimitRedisDB             *int     `yaml:"rate_limit_redis_db"`
	RateLimitRedisKeyPrefix      *string  `yaml:"rate_limit_redis_key_prefix"`
	DNSBLZones                   []string `yaml:"dnsbl_zones"`
	DNSBLCacheTTL                *string  `yaml:"dnsbl_cache_ttl"`
	DANEDNSSECTrustModel         *string  `yaml:"dane_dnssec_trust_model"`
	MTASTSCacheTTL               *string  `yaml:"mta_sts_cache_ttl"`
	MTASTSFetchTimeout           *string  `yaml:"mta_sts_fetch_timeout"`
	DeliveryMode                 *string  `yaml:"delivery_mode"`
	SpoolBackend                 *string  `yaml:"spool_backend"`
	LocalSpoolDir                *string  `yaml:"local_spool_dir"`
	SpoolS3Bucket                *string  `yaml:"spool_s3_bucket"`
	SpoolS3Prefix                *string  `yaml:"spool_s3_prefix"`
	SpoolS3Endpoint              *string  `yaml:"spool_s3_endpoint"`
	SpoolS3Region                *string  `yaml:"spool_s3_region"`
	SpoolS3AccessKey             *string  `yaml:"spool_s3_access_key"`
	SpoolS3SecretKey             *string  `yaml:"spool_s3_secret_key"`
	SpoolS3ForcePathStyle        *bool    `yaml:"spool_s3_force_path_style"`
	SpoolS3UseTLS                *bool    `yaml:"spool_s3_use_tls"`
	RelayHost                    *string  `yaml:"relay_host"`
	RelayPort                    *int     `yaml:"relay_port"`
	RelayRequireTLS              *bool    `yaml:"relay_require_tls"`
	MaxMessageBytes              *int64   `yaml:"max_message_bytes"`
	WorkerCount                  *int     `yaml:"worker_count"`
	MaxAttempts                  *int     `yaml:"max_attempts"`
	MaxRetryAge                  *string  `yaml:"max_retry_age"`
	RetrySchedule                []string `yaml:"retry_schedule"`
	ScanInterval                 *string  `yaml:"scan_interval"`
	DialTimeout                  *string  `yaml:"dial_timeout"`
	SendTimeout                  *string  `yaml:"send_timeout"`
	DKIMSignDomain               *string  `yaml:"dkim_sign_domain"`
	DKIMSignSelector             *string  `yaml:"dkim_sign_selector"`
	DKIMPrivateKeyFile           *string  `yaml:"dkim_private_key_file"`
	DKIMSignHeaders              *string  `yaml:"dkim_sign_headers"`
	ARCFailurePolicy             *string  `yaml:"arc_failure_policy"`
	SPFHeloPolicy                *string  `yaml:"spf_helo_policy"`
	SPFMailFromPolicy            *string  `yaml:"spf_mailfrom_policy"`
	DomainMaxConcurrentDefault   *int     `yaml:"domain_max_concurrent_default"`
	DomainMaxConcurrentRules     *string  `yaml:"domain_max_concurrent_rules"`
	DomainThrottleBackend        *string  `yaml:"domain_throttle_backend"`
	DomainThrottleRedisAddrs     []string `yaml:"domain_throttle_redis_addrs"`
	DomainThrottleRedisUsername  *string  `yaml:"domain_throttle_redis_username"`
	DomainThrottleRedisPassword  *string  `yaml:"domain_throttle_redis_password"`
	DomainThrottleRedisDB        *int     `yaml:"domain_throttle_redis_db"`
	DomainThrottleRedisKeyPrefix *string  `yaml:"domain_throttle_redis_key_prefix"`
	DomainAdaptiveThrottle       *bool    `yaml:"domain_adaptive_throttle"`
	DomainTempFailThreshold      *float64 `yaml:"domain_tempfail_threshold"`
	DomainPenaltyMax             *string  `yaml:"domain_penalty_max"`
	DataRetentionSent            *string  `yaml:"data_retention_sent"`
	DataRetentionDLQ             *string  `yaml:"data_retention_dlq"`
	DataRetentionPoison          *string  `yaml:"data_retention_poison"`
	RetentionSweepInterval       *string  `yaml:"retention_sweep_interval"`
	ReputationStartDate          *string  `yaml:"reputation_start_date"`
	ReputationWarmupRules        *string  `yaml:"reputation_warmup_rules"`
	ReputationBounceThreshold    *float64 `yaml:"reputation_bounce_threshold"`
	ReputationComplaintThresh    *float64 `yaml:"reputation_complaint_threshold"`
	ReputationMinSamples         *int     `yaml:"reputation_min_samples"`
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
	if raw.PolicyHook != nil {
		cfg.PolicyHook = *raw.PolicyHook
	}
	if raw.MXShaping != nil {
		cfg.MXShaping = *raw.MXShaping
	}
	if raw.QueueMaxMessages != nil {
		cfg.QueueMaxMessages = *raw.QueueMaxMessages
	}
	if raw.QueueMaxBytes != nil {
		cfg.QueueMaxBytes = *raw.QueueMaxBytes
	}
	if raw.QueueLowWatermarkPercent != nil {
		cfg.QueueLowWatermarkPercent = *raw.QueueLowWatermarkPercent
	}
	if raw.QueueMinFreeBytes != nil {
		cfg.QueueMinFreeBytes = *raw.QueueMinFreeBytes
	}

	if raw.ListenAddr != nil {
		cfg.ListenAddr = *raw.ListenAddr
	}
	if raw.SubmissionTLSAddr != nil {
		cfg.SubmissionTLSAddr = *raw.SubmissionTLSAddr
	}
	if raw.SubmissionAllowInsecureAuth != nil {
		cfg.SubmissionAllowInsecureAuth = *raw.SubmissionAllowInsecureAuth
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
	if raw.SubmissionAuthBackend != nil {
		cfg.SubmissionAuthBackend = normalizeEnum(*raw.SubmissionAuthBackend, cfg.SubmissionAuthBackend, []string{"static", "sqlite"})
	}
	if raw.SubmissionAuthDSN != nil {
		cfg.SubmissionAuthDSN = *raw.SubmissionAuthDSN
	}
	if raw.SubmissionBearerIntrospectionURL != nil {
		cfg.SubmissionBearerIntrospectionURL = *raw.SubmissionBearerIntrospectionURL
	}
	if raw.SubmissionBearerClientID != nil {
		cfg.SubmissionBearerClientID = *raw.SubmissionBearerClientID
	}
	if raw.SubmissionBearerClientSecret != nil {
		cfg.SubmissionBearerClientSecret = *raw.SubmissionBearerClientSecret
	}
	if raw.SubmissionBearerIssuer != nil {
		cfg.SubmissionBearerIssuer = *raw.SubmissionBearerIssuer
	}
	if raw.SubmissionBearerAudience != nil {
		cfg.SubmissionBearerAudience = *raw.SubmissionBearerAudience
	}
	if raw.SubmissionBearerUsernameClaim != nil {
		cfg.SubmissionBearerUsernameClaim = *raw.SubmissionBearerUsernameClaim
	}
	if raw.SubmissionBearerRequiredScope != nil {
		cfg.SubmissionBearerRequiredScope = *raw.SubmissionBearerRequiredScope
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
	if raw.OTELEnabled != nil {
		cfg.OTELEnabled = *raw.OTELEnabled
	}
	if raw.OTELServiceName != nil {
		cfg.OTELServiceName = *raw.OTELServiceName
	}
	if raw.OTELServiceVersion != nil {
		cfg.OTELServiceVersion = *raw.OTELServiceVersion
	}
	if raw.OTELExporterOTLPEndpoint != nil {
		cfg.OTELExporterOTLPEndpoint = *raw.OTELExporterOTLPEndpoint
	}
	if raw.OTELExporterOTLPInsecure != nil {
		cfg.OTELExporterOTLPInsecure = *raw.OTELExporterOTLPInsecure
	}
	if raw.OTELTraceSampleRatio != nil {
		cfg.OTELTraceSampleRatio = normalizeSampleRatio(*raw.OTELTraceSampleRatio, cfg.OTELTraceSampleRatio)
	}
	if raw.AdminAddr != nil {
		cfg.AdminAddr = *raw.AdminAddr
	}
	if raw.AdminTokens != nil {
		cfg.AdminTokens = *raw.AdminTokens
	}
	if raw.AdminAuthBackend != nil {
		cfg.AdminAuthBackend = normalizeEnum(*raw.AdminAuthBackend, cfg.AdminAuthBackend, []string{"config", "sqlite"})
	}
	if raw.AdminAuthDSN != nil {
		cfg.AdminAuthDSN = *raw.AdminAuthDSN
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
	if raw.SpoolBackend != nil {
		cfg.SpoolBackend = *raw.SpoolBackend
	}
	if raw.LocalSpoolDir != nil {
		cfg.LocalSpoolDir = *raw.LocalSpoolDir
	}
	if raw.SpoolS3Bucket != nil {
		cfg.SpoolS3Bucket = *raw.SpoolS3Bucket
	}
	if raw.SpoolS3Prefix != nil {
		cfg.SpoolS3Prefix = *raw.SpoolS3Prefix
	}
	if raw.SpoolS3Endpoint != nil {
		cfg.SpoolS3Endpoint = *raw.SpoolS3Endpoint
	}
	if raw.SpoolS3Region != nil {
		cfg.SpoolS3Region = *raw.SpoolS3Region
	}
	if raw.SpoolS3AccessKey != nil {
		cfg.SpoolS3AccessKey = *raw.SpoolS3AccessKey
	}
	if raw.SpoolS3SecretKey != nil {
		cfg.SpoolS3SecretKey = *raw.SpoolS3SecretKey
	}
	if raw.SpoolS3ForcePathStyle != nil {
		cfg.SpoolS3ForcePathStyle = *raw.SpoolS3ForcePathStyle
	}
	if raw.SpoolS3UseTLS != nil {
		cfg.SpoolS3UseTLS = *raw.SpoolS3UseTLS
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
	if raw.DomainThrottleBackend != nil {
		cfg.DomainThrottleBackend = normalizeEnum(*raw.DomainThrottleBackend, cfg.DomainThrottleBackend, []string{"memory", "redis"})
	}
	if raw.DomainThrottleRedisAddrs != nil {
		cfg.DomainThrottleRedisAddrs = append([]string(nil), raw.DomainThrottleRedisAddrs...)
	}
	if raw.DomainThrottleRedisUsername != nil {
		cfg.DomainThrottleRedisUsername = *raw.DomainThrottleRedisUsername
	}
	if raw.DomainThrottleRedisPassword != nil {
		cfg.DomainThrottleRedisPassword = *raw.DomainThrottleRedisPassword
	}
	if raw.DomainThrottleRedisDB != nil {
		cfg.DomainThrottleRedisDB = *raw.DomainThrottleRedisDB
	}
	if raw.DomainThrottleRedisKeyPrefix != nil {
		cfg.DomainThrottleRedisKeyPrefix = *raw.DomainThrottleRedisKeyPrefix
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
