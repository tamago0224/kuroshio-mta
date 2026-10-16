package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"go.opentelemetry.io/otel/attribute"
)

// handleStartTLSCommand returns a new connection after a successful upgrade.
// A nil connection and nil error means the command was refused and the current
// connection remains usable. A handshake error requires closing the connection.
func (s *Server) handleStartTLSCommand(
	ctx context.Context,
	ss *session,
	conn net.Conn,
	w *bufio.Writer,
) (net.Conn, error) {
	_, cmdSpan := startSMTPCommandSpan(ctx, "smtp.starttls")
	if ss.tls {
		cmdSpan.setResponse(503, "already using TLS")
		cmdSpan.end()
		writeResp(w, 503, "already using TLS")
		return nil, nil
	}
	if s.tlsConfig == nil {
		cmdSpan.setResponse(454, "TLS not available due to temporary reason")
		cmdSpan.end()
		writeResp(w, 454, "TLS not available due to temporary reason")
		return nil, nil
	}
	writeResp(w, 220, "Ready to start TLS")
	tlsConn := tls.Server(conn, s.tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		cmdSpan.recordError(err)
		cmdSpan.end()
		return nil, err
	}
	_ = tlsConn.SetDeadline(time.Now().Add(10 * time.Minute))
	ss.resetAfterTLS()
	cmdSpan.SetAttributes(attribute.Bool("smtp.tls", true))
	cmdSpan.setResponse(220, "Ready to start TLS")
	cmdSpan.end()
	return tlsConn, nil
}

func loadTLSConfig(cfg config.Config) (*tls.Config, error) {
	if cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		return nil, nil
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, errors.New("both MTA_TLS_CERT_FILE and MTA_TLS_KEY_FILE must be set")
	}
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS cert/key: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
