package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

// Published certificates are immutable; each handshake takes one snapshot.
type tlsCertificate struct {
	certFile, keyFile string
	current           atomic.Pointer[tls.Certificate]
}

func loadTLSConfig(cfg config.Config) (*tlsCertificate, *tls.Config, error) {
	if cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		return nil, nil, nil
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, nil, errors.New("both MTA_TLS_CERT_FILE and MTA_TLS_KEY_FILE must be set")
	}
	c := &tlsCertificate{certFile: cfg.TLSCertFile, keyFile: cfg.TLSKeyFile}
	if _, err := c.reload(time.Now()); err != nil {
		return nil, nil, err
	}
	return c, &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return c.current.Load(), nil
		},
	}, nil
}

// reload is called by the listener's single reload worker, never by handshakes.
func (c *tlsCertificate) reload(now time.Time) (bool, error) {
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return false, errors.New("TLS certificate/key cannot be loaded")
	}
	for i, der := range cert.Certificate {
		leaf, err := x509.ParseCertificate(der)
		if err != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
			return false, errors.New("TLS certificate chain is malformed or outside its validity period")
		}
		if i == 0 {
			cert.Leaf = leaf
		}
	}
	old := c.current.Load()
	if old != nil && len(old.Certificate) == len(cert.Certificate) {
		equal := true
		for i := range cert.Certificate {
			equal = equal && bytes.Equal(old.Certificate[i], cert.Certificate[i])
		}
		if equal {
			return false, nil
		}
	}
	c.current.Store(&cert)
	return true, nil
}

func (s *Server) watchCertificate(ctx context.Context) {
	prefix := "smtp_tls"
	if s.submission {
		prefix = "submission_tls"
	}
	if s.implicitTLS {
		prefix = "submission_implicit_tls"
	}
	updateExpiry := func() {
		if s.metrics != nil {
			s.metrics.SetGauge(prefix+"_certificate_not_after_seconds", s.certificate.current.Load().Leaf.NotAfter.Unix())
		}
	}
	updateExpiry()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := s.certificate.reload(time.Now())
			if err != nil {
				if s.metrics != nil {
					s.metrics.Counter(prefix + "_certificate_reload_error_total").Inc()
				}
				slog.Warn("TLS certificate reload failed; keeping previous certificate", "component", prefix)
			} else if changed {
				if s.metrics != nil {
					s.metrics.Counter(prefix + "_certificate_reload_total").Inc()
				}
				slog.Info("TLS certificate reloaded", "component", prefix)
			}
			updateExpiry()
		}
	}
}
