package config

import (
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/ingress"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

// CheckWithPath validates configuration without listeners, backend connections,
// queue locks, credential database access or writes. Errors never contain values.
func CheckWithPath(path string) error {
	_, err := LoadCheckedWithPath(path)
	return err
}

// LoadCheckedWithPath returns a complete validated snapshot without runtime side effects.
func LoadCheckedWithPath(path string) (Config, error) {
	if err := validateCheckEnvironment(); err != nil {
		return Config{}, err
	}
	cfg, err := loadWithPath(path, true)
	if err != nil {
		// YAML and TLS parsers may include credentials or input values in errors.
		return Config{}, errors.New("configuration load failed: check file availability, YAML syntax/fields/types and configuration dependencies")
	}
	if err := cfg.checkResources(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) checkResources() error {
	enums := []struct {
		field, value string
		allowed      []string
	}{
		{"queue_backend", cfg.QueueBackend, []string{"local", "kafka"}},
		{"delivery_mode", cfg.DeliveryMode, []string{"mx", "local_spool", "relay"}},
		{"spool_backend", cfg.SpoolBackend, []string{"local", "s3"}},
		{"log_level", cfg.LogLevel, []string{"debug", "info", "warn", "error"}},
	}
	cfg.QueueBackend = strings.ToLower(strings.TrimSpace(cfg.QueueBackend))
	cfg.DeliveryMode = strings.ToLower(strings.TrimSpace(cfg.DeliveryMode))
	cfg.SpoolBackend = strings.ToLower(strings.TrimSpace(cfg.SpoolBackend))
	for _, item := range enums {
		if !checkEnum(item.value, item.allowed) {
			return fmt.Errorf("invalid enum in %s", item.field)
		}
	}
	// All top-level numeric settings are nonnegative. Nested settings have their
	// own Validate methods in the shared loader.
	values := reflect.ValueOf(cfg)
	for i := range values.NumField() {
		value := values.Field(i)
		switch value.Kind() {
		case reflect.Int, reflect.Int64:
			if value.Int() < 0 {
				return fmt.Errorf("negative value in %s", values.Type().Field(i).Name)
			}
		case reflect.Float64:
			n := value.Float()
			if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
				return fmt.Errorf("invalid ratio in %s", values.Type().Field(i).Name)
			}
		}
	}
	if cfg.MaxMessageBytes == 0 || cfg.WorkerCount == 0 || cfg.ScanInterval == 0 || cfg.DialTimeout == 0 || cfg.SendTimeout == 0 {
		return errors.New("message size, worker count and scan/dial/send timeouts must be positive")
	}
	for _, duration := range cfg.RetrySchedule {
		if duration <= 0 {
			return errors.New("retry_schedule requires positive durations")
		}
	}
	if cfg.QueueLowWatermarkPercent >= 100 {
		return errors.New("queue_low_watermark_percent must be below 100")
	}
	capacity := cfg.QueueMaxMessages != 0 || cfg.QueueMaxBytes != 0 || cfg.QueueMinFreeBytes != 0
	if cfg.QueueBackend == "kafka" && capacity {
		return errors.New("queue capacity limits require local backend")
	}
	if cfg.DeliveryMode == "relay" && (strings.TrimSpace(cfg.RelayHost) == "" || cfg.RelayPort < 1 || cfg.RelayPort > 65535) {
		return errors.New("relay requires relay_host and port in 1..65535")
	}
	for _, route := range cfg.DeliveryRoutes {
		if route.RelayPasswordFile == "" {
			continue
		}
		secret, err := os.ReadFile(route.RelayPasswordFile)
		if err != nil || strings.TrimSpace(string(secret)) == "" {
			return errors.New("invalid or unreadable delivery route credential file")
		}
	}
	if cfg.DeliveryMode == "local_spool" && cfg.SpoolBackend == "s3" && cfg.SpoolS3Bucket == "" {
		return errors.New("S3 spool requires spool_s3_bucket")
	}
	addresses := []struct{ field, value string }{
		{"listen_addr", cfg.ListenAddr}, {"submission_addr", cfg.SubmissionAddr},
		{"submission_tls_addr", cfg.SubmissionTLSAddr}, {"admin_addr", cfg.AdminAddr},
		{"observability_addr", cfg.ObservabilityAddr},
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		if address.value == "" {
			continue
		}
		_, port, err := net.SplitHostPort(address.value)
		if err != nil {
			return fmt.Errorf("invalid host:port in %s", address.field)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("invalid port in %s", address.field)
		}
		// Port 0 requests an ephemeral port and does not conflict with another 0.
		if n != 0 && seen[address.value] {
			return errors.New("duplicate listener addresses")
		}
		if n != 0 {
			seen[address.value] = true
		}
	}
	if cfg.TLSCertFile != "" {
		if _, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			return errors.New("invalid or unreadable TLS certificate/key")
		}
	}
	if cfg.DKIMProfilesFile != "" {
		if err := dkim.ValidateProfilesFile(cfg.DKIMProfilesFile, cfg.Hostname); err != nil {
			return errors.New("invalid or unreadable DKIM profiles")
		}
	}
	signing := cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != ""
	if signing {
		if _, err := dkim.NewFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.DKIMSignHeaders); err != nil {
			return errors.New("invalid or unreadable DKIM signing configuration/key")
		}
	}
	if _, err := ingress.ParseRateRules(cfg.RateLimitRules); err != nil {
		return errors.New("invalid rate_limit_rules")
	}
	if cfg.PolicyHook.URL != "" && cfg.PolicyHook.TokenFile != "" {
		token, err := os.ReadFile(cfg.PolicyHook.TokenFile)
		value := strings.TrimRight(string(token), "\r\n")
		if err != nil || len(token) > 4096 || value == "" || strings.ContainsAny(value, "\r\n") {
			return errors.New("invalid or unreadable policy_hook.token_file")
		}
	}
	submission := cfg.SubmissionAddr != "" || cfg.SubmissionTLSAddr != ""
	if submission && cfg.SubmissionAuthBackend == "static" {
		if _, err := userauth.NewStatic(cfg.SubmissionUsers); err != nil {
			return errors.New("invalid submission_users")
		}
		if cfg.SubmissionAuth && strings.TrimSpace(cfg.SubmissionUsers) == "" && cfg.SubmissionBearerIntrospectionURL == "" {
			return errors.New("Submission requires password users or bearer authentication")
		}
	}
	if submission && cfg.SubmissionAuthBackend == "sqlite" && cfg.SubmissionAuthDSN == "" {
		return errors.New("SQLite Submission requires submission_auth_dsn")
	}
	if cfg.AdminAddr != "" && cfg.AdminAuthBackend == "sqlite" && cfg.AdminAuthDSN == "" {
		return errors.New("SQLite admin requires admin_auth_dsn")
	}
	if submission && cfg.SubmissionBearerIntrospectionURL != "" {
		if _, err := userauth.NewIntrospection(userauth.IntrospectionConfig{
			URL: cfg.SubmissionBearerIntrospectionURL, ClientID: cfg.SubmissionBearerClientID,
			ClientSecret: cfg.SubmissionBearerClientSecret, Issuer: cfg.SubmissionBearerIssuer,
			Audience: cfg.SubmissionBearerAudience, UsernameClaim: cfg.SubmissionBearerUsernameClaim,
			RequiredScope: cfg.SubmissionBearerRequiredScope,
		}); err != nil {
			return errors.New("invalid bearer introspection configuration")
		}
	}
	return nil
}
