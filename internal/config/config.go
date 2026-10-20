package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DeliveryPause                    DeliveryPauseConfig
	Quarantine                       QuarantineConfig
	SMTPLimits                       SMTPLimitsConfig
	ProxyProtocol                    ProxyProtocolConfig
	InboundPolicy                    InboundPolicyConfig
	PolicyHook                       PolicyHookConfig
	MXShaping                        MXShapingConfig
	ListenAddr                       string
	SubmissionAddr                   string
	SubmissionTLSAddr                string
	SubmissionAllowInsecureAuth      bool
	SubmissionAuth                   bool
	SubmissionUsers                  string
	SubmissionAuthBackend            string
	SubmissionAuthDSN                string
	SubmissionBearerIntrospectionURL string
	SubmissionBearerClientID         string
	SubmissionBearerClientSecret     string
	SubmissionBearerIssuer           string
	SubmissionBearerAudience         string
	SubmissionBearerUsernameClaim    string
	SubmissionBearerRequiredScope    string

	SubmissionSenderID           bool
	LogLevel                     string
	ObservabilityAddr            string
	OTELEnabled                  bool
	OTELServiceName              string
	OTELServiceVersion           string
	OTELExporterOTLPEndpoint     string
	OTELExporterOTLPInsecure     bool
	OTELTraceSampleRatio         float64
	AdminAddr                    string
	AdminTokens                  string
	AdminAuthBackend             string
	AdminAuthDSN                 string
	Hostname                     string
	QueueDir                     string
	QueueBackend                 string
	QueueMaxMessages             int64
	QueueMaxBytes                int64
	QueueLowWatermarkPercent     int64
	QueueMinFreeBytes            int64
	KafkaBrokers                 []string
	KafkaConsumerGroup           string
	KafkaTopicInbound            string
	KafkaTopicRetry              string
	KafkaTopicDLQ                string
	KafkaTopicSent               string
	TLSCertFile                  string
	TLSKeyFile                   string
	IngressRateLimit             int
	RateLimitRules               string
	RateLimitBackend             string
	RateLimitRedisAddrs          []string
	RateLimitRedisUsername       string
	RateLimitRedisPassword       string
	RateLimitRedisDB             int
	RateLimitRedisKeyPrefix      string
	DNSBLZones                   []string
	DNSBLCacheTTL                time.Duration
	DANEDNSSECTrustModel         string
	MTASTSCacheTTL               time.Duration
	MTASTSFetchTimeout           time.Duration
	DeliveryMode                 string
	SpoolBackend                 string
	LocalSpoolDir                string
	SpoolS3Bucket                string
	SpoolS3Prefix                string
	SpoolS3Endpoint              string
	SpoolS3Region                string
	SpoolS3AccessKey             string
	SpoolS3SecretKey             string
	SpoolS3ForcePathStyle        bool
	SpoolS3UseTLS                bool
	RelayHost                    string
	RelayPort                    int
	RelayRequireTLS              bool
	MaxMessageBytes              int64
	WorkerCount                  int
	MaxAttempts                  int
	MaxRetryAge                  time.Duration
	RetrySchedule                []time.Duration
	ScanInterval                 time.Duration
	DialTimeout                  time.Duration
	SendTimeout                  time.Duration
	DKIMProfilesFile             string
	DKIMSignDomain               string
	DKIMSignSelector             string
	DKIMPrivateKeyFile           string
	DKIMSignHeaders              string
	ARCFailurePolicy             string
	SPFHeloPolicy                string
	SPFMailFromPolicy            string
	DMARCPolicyMode              string
	DomainMaxConcurrentDefault   int
	DomainMaxConcurrentRules     string
	DomainThrottleBackend        string
	DomainThrottleRedisAddrs     []string
	DomainThrottleRedisUsername  string
	DomainThrottleRedisPassword  string
	DomainThrottleRedisDB        int
	DomainThrottleRedisKeyPrefix string
	DomainAdaptiveThrottle       bool
	DomainTempFailThreshold      float64
	DomainPenaltyMax             time.Duration
	DataRetentionSent            time.Duration
	DataRetentionDLQ             time.Duration
	DataRetentionPoison          time.Duration
	RetentionSweepInterval       time.Duration
	ReputationStartDate          string
	ReputationWarmupRules        string
	ReputationBounceThreshold    float64
	ReputationComplaintThresh    float64
	ReputationMinSamples         int
}

func Load() (Config, error) {
	return LoadWithPath("")
}

func LoadWithPath(explicitPath string) (Config, error) {
	return loadWithPath(explicitPath, false)
}

func loadWithPath(explicitPath string, strict bool) (Config, error) {
	cfg := defaultConfig()

	configPath, requestedPath, explicit := resolveConfigPath(explicitPath)
	if configPath != "" {
		loaded, err := loadYAMLConfigMode(configPath, cfg, strict)
		if err != nil {
			return Config{}, err
		}
		cfg = loaded
	} else if explicit {
		return Config{}, fmt.Errorf("config file %q not found", requestedPath)
	}

	// A malformed admission limit must not silently disable capacity protection.
	for _, key := range []string{"MTA_QUEUE_MAX_MESSAGES", "MTA_QUEUE_MAX_BYTES", "MTA_QUEUE_LOW_WATERMARK_PERCENT", "MTA_QUEUE_MIN_FREE_BYTES"} {
		if value := os.Getenv(key); value != "" {
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				return Config{}, fmt.Errorf("invalid integer for %s", key)
			}
		}
	}
	cfg = applyEnvConfig(cfg)
	if err := cfg.SMTPLimits.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.MXShaping.Validate(); err != nil {
		return Config{}, err
	}
	if cfg.DKIMProfilesFile != "" && (cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != "") {
		return Config{}, fmt.Errorf("dkim_profiles_file cannot be combined with legacy signing settings")
	}
	if err := cfg.DeliveryPause.Validate(cfg.DomainThrottleRedisAddrs); err != nil {
		return Config{}, err
	}
	if err := cfg.Quarantine.Validate(cfg.QueueBackend); err != nil {
		return Config{}, err
	}
	if err := cfg.PolicyHook.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.validateSubmissionTLS(); err != nil {
		return Config{}, err
	}
	if err := cfg.ProxyProtocol.Validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.InboundPolicy.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
