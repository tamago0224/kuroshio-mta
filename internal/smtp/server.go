package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/ingress"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"github.com/tamago0224/kuroshio-mta/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Server struct {
	cfg          config.Config
	queue        queue.Backend
	tlsConfig    *tls.Config
	tlsLoadErr   error
	limiter      *ingress.RateLimiter
	flexLimiter  *ingress.FlexibleLimiter
	rateRuleErr  error
	rateStoreErr error
	dnsbl        *ingress.DNSBLChecker
	metrics      *observability.Metrics
	ln           net.Listener
	wg           sync.WaitGroup
	submission   bool
	authBackend  userauth.Backend
}

func NewServer(cfg config.Config, q queue.Backend, metrics *observability.Metrics) *Server {
	tlsConfig, err := loadTLSConfig(cfg)
	rules, rErr := ingress.ParseRateRules(cfg.RateLimitRules)
	rateStore, storeErr := ingress.NewLimitStore(ingress.RateLimitStoreConfig{
		Backend:        cfg.RateLimitBackend,
		RedisAddrs:     cfg.RateLimitRedisAddrs,
		RedisUsername:  cfg.RateLimitRedisUsername,
		RedisPassword:  cfg.RateLimitRedisPassword,
		RedisDB:        cfg.RateLimitRedisDB,
		RedisKeyPrefix: cfg.RateLimitRedisKeyPrefix,
	})
	if storeErr != nil {
		rateStore = ingress.NewLocalLimitStore()
	}
	return &Server{
		cfg:          cfg,
		queue:        q,
		tlsConfig:    tlsConfig,
		tlsLoadErr:   err,
		limiter:      ingress.NewRateLimiterWithStore("ingress:ip", cfg.IngressRateLimit, time.Minute, rateStore),
		flexLimiter:  ingress.NewFlexibleLimiterWithStore(rules, rateStore),
		rateRuleErr:  rErr,
		rateStoreErr: storeErr,
		dnsbl:        ingress.NewDNSBLChecker(cfg.DNSBLZones, cfg.DNSBLCacheTTL, nil),
		metrics:      metrics,
	}
}

func NewSubmissionServer(cfg config.Config, q queue.Backend, metrics *observability.Metrics, backend userauth.Backend) *Server {
	subCfg := cfg
	subCfg.ListenAddr = cfg.SubmissionAddr
	s := NewServer(subCfg, q, metrics)
	s.submission = true
	s.authBackend = backend
	return s
}

func (s *Server) Run(ctx context.Context) error {
	if s.tlsLoadErr != nil {
		return s.tlsLoadErr
	}
	if s.rateRuleErr != nil {
		return s.rateRuleErr
	}
	if s.rateStoreErr != nil {
		return s.rateStoreErr
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.ln = ln
	slog.Info("smtp listening", "component", "smtp", "listen_addr", s.cfg.ListenAddr, "submission", s.submission)

	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				break
			}
			slog.Warn("smtp accept error", "component", "smtp", "error", err)
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConnWithContext(ctx, c)
		}(conn)
	}
	s.wg.Wait()
	return nil
}

type session struct {
	remote   string
	helo     string
	mailFrom string
	bodyMode string
	rcptTo   []string
	data     []byte
	seenHelo bool
	extended bool
	tls      bool
	authUser string
	auth     userauth.Principal
	authOK   bool
}

var (
	evaluateAuthWithPolicy = mailauth.EvaluateWithPolicy
	smtpTracer             = otel.Tracer("github.com/tamago0224/kuroshio-mta/internal/smtp")
)

func (s *Server) handleConn(conn net.Conn) {
	s.handleConnWithContext(context.Background(), conn)
}

func (s *Server) handleConnWithContext(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	s.metricInc("smtp_connections")
	remoteIP := parseRemoteIP(conn.RemoteAddr().String())
	spanAttrs := []attribute.KeyValue{
		attribute.Bool("smtp.submission", s.submission),
		attribute.String("smtp.remote_addr", conn.RemoteAddr().String()),
	}
	if remoteIP != nil {
		spanAttrs = append(spanAttrs, attribute.String("smtp.remote_ip", remoteIP.String()))
	}
	sessionCtx, span := smtpTracer.Start(ctx, "smtp.session", trace.WithAttributes(spanAttrs...))
	defer span.End()

	var (
		ss           *session
		rejectReason string
		sessionErr   error
	)
	defer func() {
		if ss != nil {
			span.SetAttributes(
				attribute.Bool("smtp.tls", ss.tls),
				attribute.Bool("smtp.extended", ss.extended),
				attribute.String("smtp.helo", ss.helo),
				attribute.String("smtp.mail_from", ss.mailFrom),
				attribute.Int("smtp.rcpt_count", len(ss.rcptTo)),
				attribute.String("smtp.body_mode", ss.bodyMode),
			)
		}
		if rejectReason != "" {
			span.SetAttributes(attribute.String("smtp.reject_reason", rejectReason))
			span.SetStatus(codes.Error, rejectReason)
		}
		if sessionErr != nil && !errors.Is(sessionErr, io.EOF) {
			span.RecordError(sessionErr)
			span.SetStatus(codes.Error, sessionErr.Error())
		}
	}()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if remoteIP != nil {
		remoteStr := remoteIP.String()
		now := time.Now().UTC()
		if s.limiter != nil {
			allowed, err := s.limiter.Allow(remoteStr, now)
			if err != nil {
				slog.WarnContext(ctx, "rate limit store error", "component", "smtp", "error", err, "remote_ip", remoteStr)
			} else if !allowed {
				rejectReason = "rate_limit"
				s.metricInc("smtp_reject_rate_limit")
				slog.WarnContext(ctx, "ingress rejected", "component", "smtp", "reason", "rate_limit", "remote_ip", remoteStr)
				writeResp(w, 421, "rate limit exceeded, try again later")
				return
			}
		}
		if s.flexLimiter != nil {
			allowed, err := s.flexLimiter.Allow("connect", remoteStr, "", "", now)
			if err != nil {
				slog.WarnContext(ctx, "rate limit store error", "component", "smtp", "error", err, "remote_ip", remoteStr, "event", "connect")
			} else if !allowed {
				rejectReason = "rate_rule_connect"
				s.metricInc("smtp_reject_rate_limit")
				slog.WarnContext(ctx, "ingress rejected", "component", "smtp", "reason", "rate_rule_connect", "remote_ip", remoteStr)
				writeResp(w, 421, "rate limit exceeded, try again later")
				return
			}
		}
		if s.dnsbl != nil {
			if listed, zone := s.dnsbl.IsListed(remoteStr); listed {
				rejectReason = "dnsbl:" + zone
				s.metricInc("smtp_reject_dnsbl")
				slog.WarnContext(ctx, "ingress rejected", "component", "smtp", "reason", "dnsbl", "zone", zone, "remote_ip", remoteStr)
				writeResp(w, 554, "connection rejected (dnsbl: "+zone+")")
				return
			}
		}
	}
	ss = &session{remote: conn.RemoteAddr().String(), bodyMode: "7BIT"}
	writeResp(w, 220, s.cfg.Hostname+" ESMTP ready")

	for {
		line, err := readBoundedSMTPLine(r)
		if err != nil {
			sessionErr = err
			return
		}
		if len(line) > 512 && !(s.submission && strings.HasPrefix(strings.ToUpper(line), "AUTH XOAUTH2 ") && len(line) <= maxAuthLineBytes) {
			writeResp(w, 500, "command line too long")
			continue
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			writeResp(w, 500, "empty command")
			continue
		}
		verb, arg := splitVerb(line)
		switch verb {
		case "EHLO":
			if arg == "" {
				writeResp(w, 501, "EHLO requires domain")
				continue
			}
			ss.helo = arg
			if remoteIP != nil && s.flexLimiter != nil {
				allowed, err := s.flexLimiter.Allow("helo", remoteIP.String(), ss.helo, "", time.Now().UTC())
				if err != nil {
					slog.WarnContext(ctx, "rate limit store error", "component", "smtp", "error", err, "remote_ip", remoteIP.String(), "event", "helo", "helo", ss.helo)
				} else if !allowed {
					s.metricInc("smtp_reject_rate_limit")
					slog.WarnContext(ctx, "ingress rejected", "component", "smtp", "reason", "rate_rule_helo", "remote_ip", remoteIP.String(), "helo", ss.helo)
					writeResp(w, 421, "rate limit exceeded, try again later")
					return
				}
			}
			ss.seenHelo = true
			ss.extended = true
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			methods := s.authMechanisms(ss.tls)
			writeEHLOResponse(w, s.cfg.Hostname, s.cfg.MaxMessageBytes, s.tlsConfig != nil && !ss.tls, len(methods) > 0, methods...)
		case "HELO":
			if arg == "" {
				writeResp(w, 501, "HELO requires domain")
				continue
			}
			ss.helo = arg
			ss.seenHelo = true
			ss.extended = false
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, s.cfg.Hostname)
		case "MAIL":
			_, cmdSpan := startSMTPCommandSpan(sessionCtx, "smtp.mail")
			if s.handleMailCommand(
				ctx,
				ss,
				remoteIP,
				w,
				arg,
				cmdSpan,
			) {
				return
			}
		case "RCPT":
			_, cmdSpan := startSMTPCommandSpan(sessionCtx, "smtp.rcpt")
			s.handleRcptCommand(
				ss,
				w,
				arg,
				cmdSpan,
			)
		case "DATA":
			_, cmdSpan := startSMTPCommandSpan(
				sessionCtx,
				"smtp.data",
				attribute.Int("smtp.rcpt_count", len(ss.rcptTo)),
				attribute.String("smtp.body_mode", ss.bodyMode),
				attribute.String("smtp.mail_from", logging.MaskEmail(ss.mailFrom)),
			)
			s.handleDataCommand(
				ctx,
				ss,
				remoteIP,
				r,
				w,
				cmdSpan,
			)
		case "RSET":
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "reset state")
		case "AUTH":
			if err := s.handleAuthCommand(
				ctx,
				sessionCtx,
				ss,
				r,
				w,
				arg,
			); err != nil {
				return
			}
		case "NOOP":
			writeResp(w, 250, "ok")
		case "HELP":
			help := "Supported commands: EHLO HELO MAIL RCPT DATA RSET NOOP QUIT STARTTLS HELP VRFY EXPN"
			if s.submission {
				help += " AUTH"
			}
			writeResp(w, 214, help)
		case "VRFY":
			writeResp(w, 252, "cannot VRFY user, but will accept message")
		case "EXPN":
			writeResp(w, 502, "EXPN is not supported")
		case "QUIT":
			writeResp(w, 221, "bye")
			return
		case "STARTTLS":
			_, cmdSpan := startSMTPCommandSpan(sessionCtx, "smtp.starttls")
			if ss.tls {
				cmdSpan.setResponse(503, "already using TLS")
				cmdSpan.end()
				writeResp(w, 503, "already using TLS")
				continue
			}
			if s.tlsConfig == nil {
				cmdSpan.setResponse(454, "TLS not available due to temporary reason")
				cmdSpan.end()
				writeResp(w, 454, "TLS not available due to temporary reason")
				continue
			}
			writeResp(w, 220, "Ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				cmdSpan.recordError(err)
				cmdSpan.end()
				return
			}
			conn = tlsConn
			_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			ss.tls = true
			ss.seenHelo = false
			ss.extended = false
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			ss.authUser = ""
			ss.auth = userauth.Principal{}
			ss.authOK = false
			cmdSpan.SetAttributes(attribute.Bool("smtp.tls", true))
			cmdSpan.setResponse(220, "Ready to start TLS")
			cmdSpan.end()
		default:
			writeResp(w, 500, "unsupported command")
		}
	}
}

func (s *Server) metricInc(name string) {
	if s.metrics != nil {
		s.metrics.Counter(name).Inc()
	}
}

func (s *Server) metricAuthResult(res mailauth.Result) {
	s.metricInc("smtp_auth_action_" + sanitizeMetricToken(string(res.Action)))
	s.metricInc("smtp_auth_dmarc_result_" + sanitizeMetricToken(res.DMARC.Result))
	s.metricInc("smtp_auth_dmarc_policy_" + sanitizeMetricToken(res.DMARC.Policy))
}

type smtpCommandSpan struct {
	trace.Span
}

func startSMTPCommandSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, *smtpCommandSpan) {
	cmdCtx, span := smtpTracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return cmdCtx, &smtpCommandSpan{Span: span}
}

func (s *smtpCommandSpan) setResponse(code int, message string) {
	s.SetAttributes(attribute.Int("smtp.response.code", code))
	if code >= 400 {
		s.SetStatus(codes.Error, message)
		return
	}
	s.SetStatus(codes.Ok, message)
}

func (s *smtpCommandSpan) reject(reason string, code int, message string) {
	s.SetAttributes(attribute.String("smtp.reject_reason", reason))
	s.AddEvent("smtp.reject", trace.WithAttributes(attribute.String("smtp.reject_reason", reason)))
	s.setResponse(code, message)
}

func (s *smtpCommandSpan) recordError(err error) {
	if err == nil {
		return
	}
	s.RecordError(err)
}

func (s *smtpCommandSpan) addEvent(name string, attrs ...attribute.KeyValue) {
	s.AddEvent(name, trace.WithAttributes(attrs...))
}

func (s *smtpCommandSpan) end() {
	s.End()
}

func (s *Server) enqueueDMARCReports(ctx context.Context, authRes mailauth.Result, mailFrom, msgID string, now time.Time) {
	if s.submission || s.queue == nil {
		return
	}
	fromDomain, ok := util.DomainOf(mailFrom)
	if !ok {
		return
	}
	reports := mailauth.BuildDMARCOutboundReports(authRes.DMARC, fromDomain, s.cfg.Hostname, msgID, now)
	if len(reports) == 0 {
		return
	}
	for _, rep := range reports {
		id, err := newID()
		if err != nil {
			slog.Warn("dmarc report id generation failed", "component", "smtp", "error", err, "parent_msg_id", msgID)
			continue
		}
		reportFrom := fmt.Sprintf("postmaster@%s", s.cfg.Hostname)
		msg := &model.Message{
			ID:         id,
			RemoteAddr: s.cfg.Hostname,
			Helo:       s.cfg.Hostname,
			MailFrom:   "",
			RcptTo:     []string{rep.To},
			Data:       buildReportMessage(reportFrom, rep.To, rep.Subject, rep.Body, id, now),
		}
		observability.InjectTraceContext(ctx, msg)
		if err := s.queue.Enqueue(msg); err != nil {
			slog.Warn("enqueue dmarc report failed", "component", "smtp", "error", err, "parent_msg_id", msgID, "rcpt", logging.MaskEmail(rep.To))
			continue
		}
		s.metricInc("smtp_dmarc_report_queued")
	}
}

func buildReportMessage(from, to, subject string, body []byte, msgID string, now time.Time) []byte {
	normalizedBody := strings.ReplaceAll(string(body), "\r\n", "\n")
	normalizedBody = strings.ReplaceAll(normalizedBody, "\r", "\n")
	if !strings.HasSuffix(normalizedBody, "\n") {
		normalizedBody += "\n"
	}

	var b bytes.Buffer
	b.WriteString("From: ")
	b.WriteString(sanitizeHeaderValue(from))
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(sanitizeHeaderValue(to))
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(sanitizeHeaderValue(subject))
	b.WriteString("\r\n")
	b.WriteString("Date: ")
	b.WriteString(now.UTC().Format(time.RFC1123Z))
	b.WriteString("\r\n")
	b.WriteString("Message-ID: <")
	b.WriteString(sanitizeHeaderValue(msgID))
	b.WriteString(">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(normalizedBody, "\n", "\r\n"))
	return b.Bytes()
}

func splitVerb(line string) (string, string) {
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 2)
	verb := strings.ToUpper(parts[0])
	if len(parts) == 1 {
		return verb, ""
	}
	return verb, strings.TrimSpace(parts[1])
}

func writeResp(w *bufio.Writer, code int, msg string) {
	_ = writeLine(w, fmt.Sprintf("%d %s", code, msg))
}

func writeLine(w *bufio.Writer, line string) error {
	if _, err := w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return w.Flush()
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func parseRemoteIP(remote string) net.IP {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return net.ParseIP(remote)
	}
	return net.ParseIP(host)
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

func writeEHLOResponse(w *bufio.Writer, hostname string, maxMessageBytes int64, advertiseStartTLS bool, advertiseAuth bool, mechanisms ...string) {
	_ = writeLine(w, "250-"+hostname)
	_ = writeLine(w, "250-PIPELINING")
	_ = writeLine(w, fmt.Sprintf("250-SIZE %d", maxMessageBytes))
	_ = writeLine(w, "250-8BITMIME")
	if advertiseAuth {
		if len(mechanisms) == 0 {
			mechanisms = []string{"PLAIN", "LOGIN"}
		}
		_ = writeLine(w, "250-AUTH "+strings.Join(mechanisms, " "))
	}
	if advertiseStartTLS {
		_ = writeLine(w, "250-STARTTLS")
	}
	_ = writeLine(w, "250 HELP")
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

func buildReceivedHeader(hostname, helo, remote, id string, now time.Time, extended, tlsOn bool) string {
	by := sanitizeReceivedToken(hostname)
	if by == "" {
		by = "localhost"
	}
	from := sanitizeReceivedToken(helo)
	if from == "" {
		from = "unknown"
	}
	remoteDesc := sanitizeReceivedToken(remote)
	if ip := parseRemoteIP(remote); ip != nil {
		remoteDesc = ip.String()
	}
	proto := "SMTP"
	if extended {
		proto = "ESMTP"
	}
	if tlsOn {
		proto += "S"
	}
	return fmt.Sprintf(
		"Received: from %s (%s) by %s with %s id %s; %s",
		from,
		remoteDesc,
		by,
		proto,
		sanitizeReceivedToken(id),
		now.Format(time.RFC1123Z),
	)
}

func sanitizeReceivedToken(v string) string {
	s := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", ""))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 33 || r > 126:
			b.WriteByte('_')
		case r == '(' || r == ')' || r == ';' || r == '\\' || r == ':':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 255 {
		return out[:255]
	}
	return out
}

func sanitizeHeaderValue(v string) string {
	s := strings.TrimSpace(v)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

func sanitizeMetricToken(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
