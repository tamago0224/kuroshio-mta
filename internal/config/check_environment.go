package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Strict checks run only in check-config mode; legacy startup fallback is retained.
func validateCheckEnvironment() error {
	for _, key := range []string{
		"MTA_QUEUE_MAX_MESSAGES",
		"MTA_QUEUE_MAX_BYTES",
		"MTA_QUEUE_LOW_WATERMARK_PERCENT",
		"MTA_QUEUE_MIN_FREE_BYTES",
		"MTA_MAX_MESSAGE_BYTES",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if _, err := strconv.ParseInt(os.Getenv(key), 10, 64); err != nil {
			return fmt.Errorf("invalid integer in %s", key)
		}
	}
	for _, key := range []string{
		"MTA_SUBMISSION_ALLOW_INSECURE_AUTH",
		"MTA_SUBMISSION_AUTH_REQUIRED",
		"MTA_SUBMISSION_ENFORCE_SENDER_IDENTITY",
		"MTA_OTEL_ENABLED",
		"MTA_OTEL_EXPORTER_OTLP_INSECURE",
		"MTA_SPOOL_S3_FORCE_PATH_STYLE",
		"MTA_SPOOL_S3_USE_TLS",
		"MTA_RELAY_REQUIRE_TLS",
		"MTA_DOMAIN_ADAPTIVE_THROTTLE",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on", "0", "false", "no", "off":
		default:
			return fmt.Errorf("invalid boolean in %s", key)
		}
	}
	for _, key := range []string{
		"MTA_OTEL_TRACE_SAMPLE_RATIO",
		"MTA_DOMAIN_TEMPFAIL_THRESHOLD",
		"MTA_REPUTATION_BOUNCE_THRESHOLD",
		"MTA_REPUTATION_COMPLAINT_THRESHOLD",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
			return fmt.Errorf("invalid number in %s", key)
		}
	}
	for _, key := range []string{
		"MTA_INGRESS_RATE_LIMIT_PER_MINUTE",
		"MTA_RATE_LIMIT_REDIS_DB",
		"MTA_RELAY_PORT",
		"MTA_WORKER_COUNT",
		"MTA_MAX_ATTEMPTS",
		"MTA_DOMAIN_MAX_CONCURRENT_DEFAULT",
		"MTA_DOMAIN_THROTTLE_REDIS_DB",
		"MTA_REPUTATION_MIN_SAMPLES",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if _, err := strconv.ParseInt(os.Getenv(key), 10, 64); err != nil {
			return fmt.Errorf("invalid integer in %s", key)
		}
	}
	for _, key := range []string{
		"MTA_DNSBL_CACHE_TTL",
		"MTA_MTA_STS_CACHE_TTL",
		"MTA_MTA_STS_FETCH_TIMEOUT",
		"MTA_MAX_RETRY_AGE",
		"MTA_SCAN_INTERVAL",
		"MTA_DIAL_TIMEOUT",
		"MTA_SEND_TIMEOUT",
		"MTA_DOMAIN_PENALTY_MAX",
		"MTA_DATA_RETENTION_SENT",
		"MTA_DATA_RETENTION_DLQ",
		"MTA_DATA_RETENTION_POISON",
		"MTA_RETENTION_SWEEP_INTERVAL",
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if _, err := time.ParseDuration(os.Getenv(key)); err != nil {
			return fmt.Errorf("invalid duration in %s", key)
		}
	}
	for _, key := range []string{"MTA_RETRY_SCHEDULE"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		for _, part := range strings.Split(value, ",") {
			if _, err := time.ParseDuration(strings.TrimSpace(part)); err != nil {
				return fmt.Errorf("invalid duration in %s", key)
			}
		}
	}
	if value := os.Getenv("MTA_SUBMISSION_AUTH_BACKEND"); value != "" && !checkEnum(value, []string{"static", "sqlite"}) {
		return fmt.Errorf("invalid enum in MTA_SUBMISSION_AUTH_BACKEND")
	}
	if value := os.Getenv("MTA_ADMIN_AUTH_BACKEND"); value != "" && !checkEnum(value, []string{"config", "sqlite"}) {
		return fmt.Errorf("invalid enum in MTA_ADMIN_AUTH_BACKEND")
	}
	if value := os.Getenv("MTA_RATE_LIMIT_BACKEND"); value != "" && !checkEnum(value, []string{"memory", "redis"}) {
		return fmt.Errorf("invalid enum in MTA_RATE_LIMIT_BACKEND")
	}
	if value := os.Getenv("MTA_DANE_DNSSEC_TRUST_MODEL"); value != "" && !checkEnum(value, []string{"ad_required", "insecure_allow_unsigned"}) {
		return fmt.Errorf("invalid enum in MTA_DANE_DNSSEC_TRUST_MODEL")
	}
	if value := os.Getenv("MTA_SPOOL_BACKEND"); value != "" && !checkEnum(value, []string{"local", "s3"}) {
		return fmt.Errorf("invalid enum in MTA_SPOOL_BACKEND")
	}
	if value := os.Getenv("MTA_ARC_FAILURE_POLICY"); value != "" && !checkEnum(value, []string{"accept", "quarantine", "reject"}) {
		return fmt.Errorf("invalid enum in MTA_ARC_FAILURE_POLICY")
	}
	if value := os.Getenv("MTA_SPF_HELO_POLICY"); value != "" && !checkEnum(value, []string{"off", "advisory", "enforce"}) {
		return fmt.Errorf("invalid enum in MTA_SPF_HELO_POLICY")
	}
	if value := os.Getenv("MTA_SPF_MAILFROM_POLICY"); value != "" && !checkEnum(value, []string{"off", "advisory", "enforce"}) {
		return fmt.Errorf("invalid enum in MTA_SPF_MAILFROM_POLICY")
	}
	if value := os.Getenv("MTA_DOMAIN_THROTTLE_BACKEND"); value != "" && !checkEnum(value, []string{"memory", "redis"}) {
		return fmt.Errorf("invalid enum in MTA_DOMAIN_THROTTLE_BACKEND")
	}
	for _, key := range []string{
		"MTA_SUBMISSION_USERS_FILE",
		"MTA_ADMIN_TOKENS_FILE",
		"MTA_SUBMISSION_BEARER_CLIENT_SECRET_FILE",
	} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			data, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(data)) == "" {
				return fmt.Errorf("unreadable or empty secret file in %s", key)
			}
		}
	}
	return nil
}

func checkEnum(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

func validateCheckEnums(raw yamlConfig) error {
	if raw.OTELTraceSampleRatio != nil {
		n := *raw.OTELTraceSampleRatio
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
			return fmt.Errorf("invalid ratio in otel_trace_sample_ratio")
		}
	}
	if raw.SubmissionAuthBackend != nil && !checkEnum(*raw.SubmissionAuthBackend, []string{"static", "sqlite"}) {
		return fmt.Errorf("invalid enum in SubmissionAuthBackend")
	}
	if raw.AdminAuthBackend != nil && !checkEnum(*raw.AdminAuthBackend, []string{"config", "sqlite"}) {
		return fmt.Errorf("invalid enum in AdminAuthBackend")
	}
	if raw.RateLimitBackend != nil && !checkEnum(*raw.RateLimitBackend, []string{"memory", "redis"}) {
		return fmt.Errorf("invalid enum in RateLimitBackend")
	}
	if raw.DANEDNSSECTrustModel != nil && !checkEnum(*raw.DANEDNSSECTrustModel, []string{"ad_required", "insecure_allow_unsigned"}) {
		return fmt.Errorf("invalid enum in DANEDNSSECTrustModel")
	}
	if raw.ARCFailurePolicy != nil && !checkEnum(*raw.ARCFailurePolicy, []string{"accept", "quarantine", "reject"}) {
		return fmt.Errorf("invalid enum in ARCFailurePolicy")
	}
	if raw.SPFHeloPolicy != nil && !checkEnum(*raw.SPFHeloPolicy, []string{"off", "advisory", "enforce"}) {
		return fmt.Errorf("invalid enum in SPFHeloPolicy")
	}
	if raw.SPFMailFromPolicy != nil && !checkEnum(*raw.SPFMailFromPolicy, []string{"off", "advisory", "enforce"}) {
		return fmt.Errorf("invalid enum in SPFMailFromPolicy")
	}
	if raw.DomainThrottleBackend != nil && !checkEnum(*raw.DomainThrottleBackend, []string{"memory", "redis"}) {
		return fmt.Errorf("invalid enum in DomainThrottleBackend")
	}
	return nil
}
