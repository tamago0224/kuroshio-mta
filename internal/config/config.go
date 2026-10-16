package config

import (
	"fmt"
	"time"
)

type Config struct {
	ListenAddr                       string
	SubmissionAddr                   string
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
	DKIMSignDomain               string
	DKIMSignSelector             string
	DKIMPrivateKeyFile           string
	DKIMSignHeaders              string
	ARCFailurePolicy             string
	SPFHeloPolicy                string
	SPFMailFromPolicy            string
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

	return applyEnvConfig(cfg), nil
}
