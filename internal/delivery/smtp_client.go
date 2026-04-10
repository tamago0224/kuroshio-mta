package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/router"
	"github.com/tamago0224/kuroshio-mta/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type messageSigner interface {
	Sign(raw []byte) ([]byte, error)
}

type spoolBackend interface {
	Store(*model.Message, string) error
}

type spoolBackendFunc func(*model.Message, string) error

func (f spoolBackendFunc) Store(msg *model.Message, rcpt string) error {
	return f(msg, rcpt)
}

var deliveryTracer = otel.Tracer("github.com/tamago0224/kuroshio-mta/internal/delivery")

type Client struct {
	cfg                            config.Config
	dane                           *DANEResolver
	mtaSTS                         *MTASTSResolver
	signer                         messageSigner
	arcSigner                      messageSigner
	resolveMXFn                    func(string, time.Duration) ([]router.MXHost, error)
	deliverHostFn                  func(context.Context, string, int, *model.Message, string, bool, *DANEResult) error
	spoolStore                     spoolBackend
	spoolStoreErr                  error
	reportMTASTSTestingViolationFn func(context.Context, string, string, string)
}

func NewClient(cfg config.Config) *Client {
	c := &Client{
		cfg:         cfg,
		dane:        NewDANEResolver(cfg.DialTimeout, nil),
		mtaSTS:      NewMTASTSResolver(cfg.MTASTSCacheTTL, cfg.MTASTSFetchTimeout, nil),
		resolveMXFn: router.LookupWithTimeout,
	}
	if cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != "" {
		if signer, err := dkim.NewFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.DKIMSignHeaders); err == nil {
			c.signer = signer
		}
		if arcSigner, err := dkim.NewARCFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.Hostname, cfg.DKIMSignHeaders); err == nil {
			c.arcSigner = arcSigner
		}
	}
	c.deliverHostFn = c.deliverHost
	c.spoolStore, c.spoolStoreErr = c.newSpoolBackend()
	c.reportMTASTSTestingViolationFn = func(ctx context.Context, domain, host, reason string) {
		slog.WarnContext(ctx, "mta-sts testing policy violation",
			"domain", domain,
			"host", host,
			"reason", reason,
		)
	}
	return c
}

func (c *Client) Deliver(ctx context.Context, msg *model.Message, rcpt string) error {
	rcptDomain, _ := util.DomainOf(rcpt)
	mode := strings.ToLower(strings.TrimSpace(c.cfg.DeliveryMode))
	if mode == "" {
		mode = "mx"
	}
	ctx, span := deliveryTracer.Start(ctx, "delivery.deliver")
	span.SetAttributes(
		attribute.String("delivery.mode", mode),
		attribute.String("mail.message_id", msg.ID),
		attribute.Int("mail.attempt", msg.Attempts),
		attribute.String("mail.rcpt_domain", rcptDomain),
	)
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}()

	switch mode {
	case "mx":
		err = c.deliverByMX(ctx, msg, rcpt)
	case "local_spool":
		if c.spoolStoreErr != nil {
			err = c.spoolStoreErr
			break
		}
		if c.spoolStore == nil {
			err = errors.New("spool backend is not configured")
			break
		}
		err = c.spoolStore.Store(msg, rcpt)
	case "relay":
		if strings.TrimSpace(c.cfg.RelayHost) == "" {
			err = errors.New("relay mode requires MTA_RELAY_HOST")
			break
		}
		port := c.cfg.RelayPort
		if port <= 0 {
			port = 25
		}
		err = c.deliverHostFn(ctx, c.cfg.RelayHost, port, msg, rcpt, c.cfg.RelayRequireTLS, nil)
	default:
		err = fmt.Errorf("unknown delivery mode: %s", mode)
	}
	return err
}

func (c *Client) newSpoolBackend() (spoolBackend, error) {
	backend := strings.ToLower(strings.TrimSpace(c.cfg.SpoolBackend))
	if backend == "" {
		backend = "local"
	}
	switch backend {
	case "local":
		return spoolBackendFunc(c.writeLocalSpool), nil
	case "s3":
		return newS3SpoolStore(c.cfg, c.prepareOutboundData)
	default:
		return nil, fmt.Errorf("unknown spool backend: %s", backend)
	}
}

func (c *Client) deliverByMX(ctx context.Context, msg *model.Message, rcpt string) error {
	ctx, span := deliveryTracer.Start(ctx, "delivery.mx")
	defer span.End()

	domain, ok := util.DomainOf(rcpt)
	if !ok {
		err := errors.New("invalid rcpt domain")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.String("mail.rcpt_domain", domain))

	_, lookupSpan := deliveryTracer.Start(ctx, "delivery.lookup_mx", trace.WithAttributes(
		attribute.String("mail.rcpt_domain", domain),
	))
	mxHosts, err := c.resolveMXFn(domain, c.cfg.DialTimeout)
	if err == nil {
		lookupSpan.SetAttributes(attribute.Int("delivery.mx_count", len(mxHosts)))
	}
	if err != nil {
		lookupSpan.RecordError(err)
		lookupSpan.SetStatus(codes.Error, err.Error())
	}
	lookupSpan.End()
	if err != nil {
		return fmt.Errorf("mx lookup failed: %w", err)
	}
	requireTLS := false
	daneActive := false
	daneByHost := map[string]DANEResult{}
	var mtaSTSTestingPolicy *MTASTSPolicy
	daneCandidates := make([]router.MXHost, 0, len(mxHosts))
	if c.dane != nil {
		for _, mx := range mxHosts {
			res, lErr := c.dane.LookupHost(ctx, mx.Host, 25)
			if lErr != nil {
				continue
			}
			if res.HasUsableTLSAWithTrustModel(c.cfg.DANEDNSSECTrustModel) {
				daneCandidates = append(daneCandidates, mx)
				daneByHost[mx.Host] = res
			}
		}
	}
	if len(daneCandidates) > 0 {
		// RFC 7672 precedence: if usable DANE is available, apply it before MTA-STS.
		requireTLS = true
		daneActive = true
		mxHosts = daneCandidates
	} else if c.mtaSTS != nil {
		if p, pErr := c.mtaSTS.Lookup(ctx, domain); pErr == nil {
			if p.Mode == "enforce" {
				requireTLS = true
				filtered := make([]router.MXHost, 0, len(mxHosts))
				for _, mx := range mxHosts {
					if p.AllowsMX(mx.Host) {
						filtered = append(filtered, mx)
					}
				}
				if len(filtered) == 0 {
					return &SMTPResponseError{Code: 454, Line: "mta-sts policy mismatch: no allowed mx"}
				}
				mxHosts = filtered
			} else if p.Mode == "testing" {
				cp := p
				mtaSTSTestingPolicy = &cp
			}
		}
	}
	var lastErr error
	for _, mx := range mxHosts {
		if mtaSTSTestingPolicy != nil && !mtaSTSTestingPolicy.AllowsMX(mx.Host) && c.reportMTASTSTestingViolationFn != nil {
			c.reportMTASTSTestingViolationFn(ctx, domain, mx.Host, "mx_mismatch")
		}
		var daneRes *DANEResult
		if daneActive {
			if res, ok := daneByHost[mx.Host]; ok {
				cp := res
				daneRes = &cp
			}
		}
		attemptCtx, attemptSpan := deliveryTracer.Start(ctx, "delivery.attempt_host")
		attemptSpan.SetAttributes(
			attribute.String("delivery.mx_host", mx.Host),
			attribute.Bool("delivery.require_tls", requireTLS),
			attribute.Bool("delivery.dane_active", daneActive),
		)
		err := c.deliverHostFn(attemptCtx, mx.Host, 25, msg, rcpt, requireTLS, daneRes)
		if daneActive {
			err = classifyDANEFailure(err)
		}
		if err != nil {
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, err.Error())
		}
		attemptSpan.End()
		if err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no mx targets")
	}
	return lastErr
}

func classifyDANEFailure(err error) error {
	if err == nil {
		return nil
	}
	var smtpErr *SMTPResponseError
	if !errors.As(err, &smtpErr) {
		return err
	}
	line := strings.ToLower(strings.TrimSpace(smtpErr.Line))
	if smtpErr.Temporary() && (line == "starttls handshake failed" || line == "mta-sts enforce requires starttls") {
		return &SMTPResponseError{Code: 550, Line: "dane authentication failed"}
	}
	return err
}

func (c *Client) deliverHost(ctx context.Context, host string, port int, msg *model.Message, rcpt string, requireTLS bool, daneRes *DANEResult) error {
	ctx, span := deliveryTracer.Start(ctx, "delivery.smtp_attempt")
	span.SetAttributes(
		attribute.String("delivery.host", host),
		attribute.Int("delivery.port", port),
		attribute.Bool("delivery.require_tls", requireTLS),
		attribute.Bool("delivery.dane_tlsa", daneRes != nil && len(daneRes.Records) > 0),
	)
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}()

	if port <= 0 {
		port = 25
	}
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.SendTimeout))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if _, _, err := readCode(r); err != nil {
		return err
	}

	if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
		return err
	}
	ehloLines, err := readLines(r)
	if err != nil {
		return err
	}
	startTLS := false
	for _, l := range ehloLines {
		if strings.Contains(strings.ToUpper(l), "STARTTLS") {
			startTLS = true
			break
		}
	}

	if startTLS {
		if err := writeLine(w, "STARTTLS"); err != nil {
			return err
		}
		code, _, err := readCode(r)
		if err != nil {
			return err
		}
		if code == 220 {
			tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				if requireTLS {
					return &SMTPResponseError{Code: 454, Line: "starttls handshake failed"}
				}
				return err
			}
			if daneRes != nil {
				state := tlsConn.ConnectionState()
				if err := verifyPeerCertificatesWithTLSA(host, state.PeerCertificates, daneRes.Records); err != nil {
					return &SMTPResponseError{Code: 550, Line: "dane authentication failed"}
				}
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
				return err
			}
			if _, err := readLines(r); err != nil {
				return err
			}
		}
	} else if requireTLS {
		return &SMTPResponseError{Code: 454, Line: "mta-sts enforce requires starttls"}
	}

	if err := writeLine(w, fmt.Sprintf("MAIL FROM:<%s>", msg.MailFrom)); err != nil {
		return err
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("mail from rejected: %w", err)
	}

	if err := writeLine(w, fmt.Sprintf("RCPT TO:<%s>", rcpt)); err != nil {
		return err
	}
	if err := expect2xx3xx(r); err != nil {
		return fmt.Errorf("rcpt rejected: %w", err)
	}

	if err := writeLine(w, "DATA"); err != nil {
		return err
	}
	if err := expectCode(r, 354); err != nil {
		return fmt.Errorf("data not accepted: %w", err)
	}

	payload, err := c.prepareOutboundData(msg.Data)
	if err != nil {
		return err
	}
	data := dotStuff(payload)
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.WriteString("\r\n.\r\n"); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("final delivery reject: %w", err)
	}
	_ = writeLine(w, "QUIT")
	_, _, _ = readCode(r)
	return nil
}

func (c *Client) writeLocalSpool(msg *model.Message, rcpt string) error {
	dir := strings.TrimSpace(c.cfg.LocalSpoolDir)
	if dir == "" {
		dir = "./var/spool"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	name := fmt.Sprintf("%s_%s.eml", id, sanitizeFilename(rcpt))
	path := filepath.Join(dir, name)
	payload, err := c.prepareOutboundData(msg.Data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func sanitizeFilename(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func writeLine(w *bufio.Writer, line string) error {
	if _, err := w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return w.Flush()
}

func readCode(r *bufio.Reader) (int, string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, line, errors.New("short smtp response")
	}
	var code int
	_, err = fmt.Sscanf(line[:3], "%d", &code)
	if err != nil {
		return 0, line, err
	}
	return code, line, nil
}

func readLines(r *bufio.Reader) ([]string, error) {
	var out []string
	for {
		code, line, err := readCode(r)
		if err != nil {
			return nil, err
		}
		if code < 200 || code > 399 {
			return nil, &SMTPResponseError{Code: code, Line: line}
		}
		out = append(out, line)
		if len(line) >= 4 && line[3] == ' ' {
			return out, nil
		}
	}
}

func expect2xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func expect2xx3xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if (code < 200 || code > 299) && (code < 300 || code > 399) {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func expectCode(r *bufio.Reader, expected int) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code != expected {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func dotStuff(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
		if strings.HasPrefix(lines[i], ".") {
			lines[i] = "." + lines[i]
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}

func (c *Client) prepareOutboundData(raw []byte) ([]byte, error) {
	signed := raw
	if c.signer != nil {
		var err error
		signed, err = c.signer.Sign(signed)
		if err != nil {
			return nil, err
		}
	}
	if c.arcSigner != nil {
		var err error
		signed, err = c.arcSigner.Sign(signed)
		if err != nil {
			return nil, err
		}
	}
	return signed, nil
}
