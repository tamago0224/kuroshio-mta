package config

import (
	"crypto/tls"
	"fmt"
)

func (cfg Config) validateSubmissionTLS() error {
	enabled := cfg.SubmissionAddr != "" || cfg.SubmissionTLSAddr != ""
	required := cfg.SubmissionTLSAddr != "" || (enabled && !cfg.SubmissionAllowInsecureAuth)
	if cfg.SubmissionTLSAddr != "" && (cfg.SubmissionTLSAddr == cfg.SubmissionAddr || cfg.SubmissionTLSAddr == cfg.ListenAddr) {
		return fmt.Errorf("submission_tls_addr must differ from other SMTP listeners")
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return fmt.Errorf("both tls_cert_file and tls_key_file must be set")
	}
	if required && cfg.TLSCertFile == "" {
		return fmt.Errorf("Submission requires tls_cert_file and tls_key_file; submission_allow_insecure_auth is for isolated testing only")
	}
	if enabled && cfg.TLSCertFile != "" {
		if _, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			return fmt.Errorf("load Submission TLS certificate/key: %w", err)
		}
	}
	return nil
}
