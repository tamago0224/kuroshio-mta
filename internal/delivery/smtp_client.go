package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/router"
	"github.com/tamago0224/kuroshio-mta/internal/shaping"
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
	Store(context.Context, *model.Message, string) (int, error)
}

type spoolBackendFunc func(context.Context, *model.Message, string) (int, error)

func (f spoolBackendFunc) Store(ctx context.Context, msg *model.Message, rcpt string) (int, error) {
	return f(ctx, msg, rcpt)
}

var deliveryTracer = otel.Tracer("github.com/tamago0224/kuroshio-mta/internal/delivery")

type Client struct {
	profiles                       *dkim.ProfileManager
	shaper                         *shaping.Shaper
	shapingErr                     error
	signingErr                     error
	routingErr                     error
	cfg                            config.Config
	dane                           *DANEResolver
	mtaSTS                         *MTASTSResolver
	signer                         messageSigner
	arcSigner                      messageSigner
	resolveMXFn                    func(string, time.Duration) ([]router.MXHost, error)
	resolveHostFn                  func(context.Context, string) ([]string, error)
	deliverHostFn                  func(context.Context, string, int, *model.Message, string, bool, *DANEResult) error
	deliverLMTPFn                  func(context.Context, string, *model.Message, []string) map[string]error
	tlsConfigFn                    func(string) *tls.Config
	spoolStore                     spoolBackend
	spoolStoreErr                  error
	reportMTASTSTestingViolationFn func(context.Context, string, string, string)
	metrics                        *observability.Metrics
	routes                         []deliveryRoute
}

func NewClient(cfg config.Config) *Client {
	return NewClientWithMetrics(cfg, nil)
}

func NewClientWithMetrics(cfg config.Config, metrics *observability.Metrics) *Client {
	return NewClientWithContext(context.TODO(), cfg, metrics)
}

// NewClientWithContext propagates startup cancellation into profile DNS checks.
func NewClientWithContext(ctx context.Context, cfg config.Config, metrics *observability.Metrics) *Client {
	c := &Client{
		cfg:         cfg,
		dane:        NewDANEResolver(cfg.DialTimeout, nil),
		mtaSTS:      NewMTASTSResolver(cfg.MTASTSCacheTTL, cfg.MTASTSFetchTimeout, nil),
		resolveMXFn: router.LookupWithTimeout,
		metrics:     metrics,
	}
	if cfg.DNSCacheEnabled {
		resolver := router.NewCachedResolver(router.DNSCacheConfig{
			Enabled: true, PositiveTTL: cfg.DNSCachePositiveTTL,
			NegativeTTL: cfg.DNSCacheNegativeTTL, Capacity: cfg.DNSCacheCapacity,
		}, metrics)
		c.resolveMXFn = resolver.LookupWithTimeout
		c.resolveHostFn = resolver.LookupHost
	}
	c.routes, c.routingErr = loadDeliveryRoutes(cfg.DeliveryRoutes)
	if cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != "" {
		if signer, err := dkim.NewFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.DKIMSignHeaders); err == nil {
			c.signer = signer
		} else {
			c.signingErr = errors.New("configured DKIM signer initialization failed")
		}
		if arcSigner, err := dkim.NewARCFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.Hostname, cfg.DKIMSignHeaders); err == nil {
			c.arcSigner = &validatedARCSigner{signer: arcSigner}
		} else {
			c.signingErr = errors.Join(c.signingErr, errors.New("configured ARC signer initialization failed"))
		}
	}
	if cfg.DKIMProfilesFile != "" {
		if cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != "" {
			c.signingErr = errors.New("profiles cannot be combined with legacy signing settings")
		} else {
			manager, err := dkim.NewProfileManager(ctx, cfg.DKIMProfilesFile, cfg.Hostname, nil)
			if err != nil {
				c.signingErr = err
			} else {
				c.profiles = manager
			}
		}
	}
	c.shaper, c.shapingErr = shaping.New(cfg, metrics)
	c.deliverHostFn = c.deliverHost
	c.deliverLMTPFn = c.deliverLMTP
	c.tlsConfigFn = func(host string) *tls.Config {
		return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
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

func (c *Client) InitError() error { return errors.Join(c.shapingErr, c.signingErr, c.routingErr) }
func (c *Client) Close() error     { return c.shaper.Close() }

type shapingContextKey struct{}
type shapingAttempt struct {
	key             string
	limits          config.ShapingLimits
	messageReserved bool
}

func (c *Client) Deliver(ctx context.Context, msg *model.Message, rcpt string) error {
	if err := c.InitError(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rcptDomain, _ := util.DomainOf(rcpt)
	mode := strings.ToLower(strings.TrimSpace(c.cfg.DeliveryMode))
	route, routed := c.routeFor(rcptDomain)
	if routed {
		mode = strings.ToLower(strings.TrimSpace(route.Mode))
	}
	if mode == "" {
		mode = "mx"
	}
	ctx, span := deliveryTracer.Start(ctx, "delivery.deliver")
	span.SetAttributes(
		attribute.String("delivery.mode", mode),
		attribute.String("mail.message_id", msg.ID),
		attribute.Int("mail.attempt", msg.Attempts),
		attribute.String("mail.rcpt_domain", rcptDomain),
		attribute.String("delivery.route", route.Name),
	)
	defer span.End()
	if routed {
		slog.InfoContext(ctx, "delivery route selected",
			"component", "audit",
			"event", "delivery_route_selected",
			"msg_id", msg.ID,
			"rcpt", logging.MaskEmail(rcpt),
			"route", route.Name,
			"mode", mode,
		)
	}

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
		err = c.storeToSpool(ctx, msg, rcpt)
	case "relay":
		host, port, requireTLS := c.cfg.RelayHost, c.cfg.RelayPort, c.cfg.RelayRequireTLS
		if routed {
			host, port, requireTLS = route.RelayHost, route.RelayPort, route.RelayRequireTLS
			if route.RelayUsername != "" {
				ctx = context.WithValue(ctx, relayAuthContextKey{}, relayAuth{username: route.RelayUsername, password: route.password})
			}
		}
		if strings.TrimSpace(host) == "" {
			err = errors.New("relay mode requires MTA_RELAY_HOST")
			break
		}
		if port <= 0 {
			port = 25
		}
		if c.shaper != nil {
			key, limits := c.shaper.Group(rcptDomain, []router.MXHost{{Host: host}})
			ctx = context.WithValue(ctx, shapingContextKey{}, &shapingAttempt{key: key, limits: limits})
		}
		err = c.deliverHostFn(ctx, host, port, msg, rcpt, requireTLS, nil)
	case "lmtp":
		if !routed {
			err = errors.New("LMTP requires a named delivery route")
			break
		}
		err = c.deliverLMTPFn(ctx, route.LMTPUnixSocket, msg, []string{rcpt})[rcpt]
	default:
		err = fmt.Errorf("unknown delivery mode: %s", mode)
	}
	return err
}

// BatchKey identifies recipients that can share one multi-recipient delivery.
func (c *Client) BatchKey(rcpt string) (string, bool) {
	domain, ok := util.DomainOf(rcpt)
	if !ok {
		return "", false
	}
	route, ok := c.routeFor(domain)
	if !ok || !strings.EqualFold(strings.TrimSpace(route.Mode), "lmtp") {
		return "", false
	}
	return route.Name, true
}

// DeliverBatch sends recipients on one LMTP route in a single transaction.
func (c *Client) DeliverBatch(ctx context.Context, msg *model.Message, recipients []string) map[string]error {
	results := make(map[string]error, len(recipients))
	if len(recipients) == 0 {
		return results
	}
	if err := c.InitError(); err != nil {
		for _, rcpt := range recipients {
			results[rcpt] = err
		}
		return results
	}
	if err := ctx.Err(); err != nil {
		for _, rcpt := range recipients {
			results[rcpt] = err
		}
		return results
	}
	domain, ok := util.DomainOf(recipients[0])
	if !ok {
		results[recipients[0]] = errors.New("invalid recipient domain")
		return results
	}
	route, ok := c.routeFor(domain)
	if !ok || !strings.EqualFold(strings.TrimSpace(route.Mode), "lmtp") {
		for _, rcpt := range recipients {
			results[rcpt] = c.Deliver(ctx, msg, rcpt)
		}
		return results
	}
	for _, rcpt := range recipients[1:] {
		key, matches := c.BatchKey(rcpt)
		if !matches || key != route.Name {
			results[rcpt] = errors.New("LMTP batch contains recipients from different routes")
			return results
		}
	}
	ctx, span := deliveryTracer.Start(ctx, "delivery.lmtp_batch")
	span.SetAttributes(
		attribute.String("delivery.mode", "lmtp"),
		attribute.String("delivery.route", route.Name),
		attribute.String("mail.message_id", msg.ID),
		attribute.Int("mail.rcpt_count", len(recipients)),
	)
	defer span.End()
	for _, rcpt := range recipients {
		slog.InfoContext(ctx, "delivery route selected",
			"component", "audit",
			"event", "delivery_route_selected",
			"msg_id", msg.ID,
			"rcpt", logging.MaskEmail(rcpt),
			"route", route.Name,
			"mode", "lmtp",
		)
	}
	results = c.deliverLMTPFn(ctx, route.LMTPUnixSocket, msg, recipients)
	for _, err := range results {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "one or more LMTP recipients failed")
			break
		}
	}
	return results
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
		return newS3SpoolStore(c.cfg, c.prepareOutboundMessage)
	default:
		return nil, fmt.Errorf("unknown spool backend: %s", backend)
	}
}

func (c *Client) storeToSpool(ctx context.Context, msg *model.Message, rcpt string) error {
	backend := strings.ToLower(strings.TrimSpace(c.cfg.SpoolBackend))
	if backend == "" {
		backend = "local"
	}
	ctx, span := deliveryTracer.Start(ctx, "delivery.spool_store")
	span.SetAttributes(
		attribute.String("delivery.spool_backend", backend),
		attribute.String("mail.message_id", msg.ID),
	)
	defer span.End()

	size, err := c.spoolStore.Store(ctx, msg, rcpt)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.metricInc("delivery_spool_store_error")
		c.metricInc("delivery_spool_store_" + backend + "_error")
		return err
	}

	span.SetAttributes(attribute.Int("mail.size_bytes", size))
	c.metricInc("delivery_spool_store")
	c.metricInc("delivery_spool_store_" + backend)
	if size > 0 {
		c.metricAdd("delivery_spool_store_bytes", uint64(size))
		c.metricAdd("delivery_spool_store_"+backend+"_bytes", uint64(size))
	}
	return nil
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
	var mxHosts []router.MXHost
	var err error
	if c.shaper != nil {
		mxHosts, err = c.shaper.Lookup(ctx, domain, c.resolveMXFn, c.cfg.DialTimeout)
	} else {
		mxHosts, err = c.resolveMXFn(domain, c.cfg.DialTimeout)
	}
	if err == nil {
		lookupSpan.SetAttributes(attribute.Int("delivery.mx_count", len(mxHosts)))
	}
	if err != nil {
		lookupSpan.RecordError(err)
		lookupSpan.SetStatus(codes.Error, err.Error())
	}
	lookupSpan.End()
	if err != nil {
		return fmt.Errorf("mx lookup failed: %w", atStage(StageDNS, err))
	}
	if c.shaper != nil {
		key, limits := c.shaper.Group(domain, mxHosts)
		ctx = context.WithValue(ctx, shapingContextKey{}, &shapingAttempt{key: key, limits: limits})
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
					return &SMTPResponseError{Stage: StageTLS, Code: 454, Line: "mta-sts policy mismatch: no allowed mx"}
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
			var deferred *shaping.DeferredError
			if errors.As(err, &deferred) {
				// A real earlier host failure still consumes an attempt; a pure
				// shaping wait does not. Preserve that failure instead of hiding it.
				if lastErr != nil {
					return lastErr
				}
				return err
			}
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
		return &SMTPResponseError{Stage: StageTLS, Code: 550, Line: "dane authentication failed"}
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
	if c.shaper != nil {
		attempt, ok := ctx.Value(shapingContextKey{}).(*shapingAttempt)
		if !ok {
			return errors.New("MX shaping context missing")
		}
		release, err := c.shaper.Acquire(ctx, attempt.key, attempt.limits, !attempt.messageReserved)
		if err != nil {
			return err
		}
		attempt.messageReserved = true
		defer release()
	}
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	addresses := []string{host}
	if c.resolveHostFn != nil && net.ParseIP(host) == nil {
		lookupCtx, cancel := context.WithTimeout(ctx, c.cfg.DialTimeout)
		addresses, err = c.resolveHostFn(lookupCtx, host)
		cancel()
		if err != nil || len(addresses) == 0 {
			if err == nil {
				err = errors.New("address lookup returned no results")
			}
			return atStage(StageDNS, err)
		}
	}
	var conn net.Conn
	for _, address := range addresses {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprintf("%d", port)))
		if err == nil {
			break
		}
	}
	if err != nil {
		return atStage(StageConnect, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.SendTimeout))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if err := expectCode(r, 220); err != nil {
		return atStage(StageGreeting, err)
	}

	if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
		return atStage(StageEHLO, err)
	}
	ehloLines, err := readLines(r)
	if err != nil {
		return atStage(StageEHLO, err)
	}
	startTLS, tlsActive := false, false
	for _, l := range ehloLines {
		if strings.Contains(strings.ToUpper(l), "STARTTLS") {
			startTLS = true
			break
		}
	}

	if startTLS {
		if err := writeLine(w, "STARTTLS"); err != nil {
			return atStage(StageTLS, err)
		}
		code, line, err := readCode(r)
		if err != nil {
			return atStage(StageTLS, err)
		}
		if code != 220 && requireTLS {
			return atStage(StageTLS, &SMTPResponseError{Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)})
		}
		if code == 220 {
			tlsConn := tls.Client(conn, c.tlsConfigFn(host))
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				if requireTLS {
					return &SMTPResponseError{Stage: StageTLS, Code: 454, Line: "starttls handshake failed"}
				}
				return atStage(StageTLS, err)
			}
			if daneRes != nil {
				state := tlsConn.ConnectionState()
				if err := verifyPeerCertificatesWithTLSA(host, state.PeerCertificates, daneRes.Records); err != nil {
					return &SMTPResponseError{Stage: StageTLS, Code: 550, Line: "dane authentication failed"}
				}
			}
			conn = tlsConn
			tlsActive = true
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
				return atStage(StageEHLO, err)
			}
			ehloLines, err = readLines(r)
			if err != nil {
				return atStage(StageEHLO, err)
			}
		}
	} else if requireTLS {
		return &SMTPResponseError{Stage: StageTLS, Code: 454, Line: "mta-sts enforce requires starttls"}
	}

	if auth, ok := ctx.Value(relayAuthContextKey{}).(relayAuth); ok {
		if !tlsActive {
			return &SMTPResponseError{Stage: StageTLS, Code: 454, Line: "relay authentication requires verified TLS"}
		}
		if !supportsAuthPlain(ehloLines) {
			return &SMTPResponseError{Stage: StageAUTH, Code: 504, Line: "relay does not advertise AUTH PLAIN"}
		}
		payload := base64.StdEncoding.EncodeToString([]byte("\x00" + auth.username + "\x00" + auth.password))
		if err := writeLine(w, "AUTH PLAIN "+payload); err != nil {
			return atStage(StageAUTH, err)
		}
		code, line, err := readCode(r)
		if err != nil {
			return atStage(StageAUTH, err)
		}
		if code != 235 {
			return &SMTPResponseError{Stage: StageAUTH, Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
		}
	}

	if err := writeLine(w, fmt.Sprintf("MAIL FROM:<%s>", msg.MailFrom)); err != nil {
		return atStage(StageMAIL, err)
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("mail from rejected: %w", atStage(StageMAIL, err))
	}

	if err := writeLine(w, fmt.Sprintf("RCPT TO:<%s>", rcpt)); err != nil {
		return atStage(StageRCPT, err)
	}
	if err := expect2xx3xx(r); err != nil {
		return fmt.Errorf("rcpt rejected: %w", atStage(StageRCPT, err))
	}

	if err := writeLine(w, "DATA"); err != nil {
		return atStage(StageDATA, err)
	}
	if err := expectCode(r, 354); err != nil {
		return fmt.Errorf("data not accepted: %w", atStage(StageDATA, err))
	}

	payload, err := c.prepareOutboundMessage(ctx, msg)
	if err != nil {
		return err
	}
	data := dotStuff(payload)
	if _, err := w.Write(data); err != nil {
		return atStage(StageDATA, err)
	}
	if _, err := w.WriteString("\r\n.\r\n"); err != nil {
		return atStage(StageDATA, err)
	}
	if err := w.Flush(); err != nil {
		return atStage(StageDATA, err)
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("final delivery reject: %w", atStage(StageDATA, err))
	}
	_ = writeLine(w, "QUIT")
	_, _, _ = readCode(r)
	return nil
}

func supportsAuthPlain(lines []string) bool {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) >= 4 && line[0] >= '0' && line[0] <= '9' && line[1] >= '0' && line[1] <= '9' && line[2] >= '0' && line[2] <= '9' && (line[3] == ' ' || line[3] == '-') {
			line = line[4:]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.EqualFold(fields[0], "AUTH") {
			continue
		}
		for _, mechanism := range fields[1:] {
			if strings.EqualFold(mechanism, "PLAIN") {
				return true
			}
		}
	}
	return false
}

func (c *Client) writeLocalSpool(ctx context.Context, msg *model.Message, rcpt string) (int, error) {
	dir := strings.TrimSpace(c.cfg.LocalSpoolDir)
	if dir == "" {
		dir = "./var/spool"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	name := fmt.Sprintf("%s_%s.eml", id, sanitizeFilename(rcpt))
	path := filepath.Join(dir, name)
	payload, err := c.prepareOutboundMessage(ctx, msg)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *Client) metricInc(name string) {
	if c.metrics == nil {
		return
	}
	c.metrics.Counter(name).Inc()
}

func (c *Client) metricAdd(name string, n uint64) {
	if c.metrics == nil {
		return
	}
	c.metrics.Counter(name).Add(n)
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
	code, message, err := textproto.NewReader(r).ReadResponse(0)
	if err != nil {
		return 0, "", err
	}
	parts := strings.Split(message, "\n")
	for i, part := range parts {
		sep := "-"
		if i == len(parts)-1 {
			sep = " "
		}
		parts[i] = fmt.Sprintf("%03d%s%s", code, sep, part)
	}
	return code, strings.Join(parts, "\n"), nil
}

func readLines(r *bufio.Reader) ([]string, error) {
	code, line, err := readCode(r)
	if err != nil {
		return nil, err
	}
	if code < 200 || code > 399 {
		return nil, &SMTPResponseError{Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
	}
	return strings.Split(line, "\n"), nil
}

func expect2xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return &SMTPResponseError{Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
	}
	return nil
}

func expect2xx3xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if (code < 200 || code > 299) && (code < 300 || code > 399) {
		return &SMTPResponseError{Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
	}
	return nil
}

func expectCode(r *bufio.Reader, expected int) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code != expected {
		return &SMTPResponseError{Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
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
	if c.profiles != nil {
		return nil, errors.New("profile signing requires trusted message identity")
	}
	if c.signingErr != nil {
		return nil, c.signingErr
	}
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
