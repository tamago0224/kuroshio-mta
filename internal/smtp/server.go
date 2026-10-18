package smtp

import (
	"bufio"
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
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/policyhook"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Server struct {
	policyHook       policyhook.Evaluator
	policyHookClient *policyhook.Client
	policyHookErr    error
	cfg              config.Config
	queue            queue.Backend
	tlsConfig        *tls.Config
	tlsLoadErr       error
	limiter          *ingress.RateLimiter
	flexLimiter      *ingress.FlexibleLimiter
	rateRuleErr      error
	rateStoreErr     error
	dnsbl            *ingress.DNSBLChecker
	metrics          *observability.Metrics
	ln               net.Listener
	wg               sync.WaitGroup
	submission       bool
	implicitTLS      bool
	authBackend      userauth.Backend
}

func NewServer(cfg config.Config, q queue.Backend, metrics *observability.Metrics) *Server {
	hook, hookErr := policyhook.New(cfg.PolicyHook, metrics)
	var evaluator policyhook.Evaluator
	if hook != nil {
		evaluator = hook
	}
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
		policyHook: evaluator, policyHookClient: hook, policyHookErr: hookErr,
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

// NewImplicitTLSSubmissionServer shares Submission authentication and sender policy.
func NewImplicitTLSSubmissionServer(cfg config.Config, q queue.Backend, metrics *observability.Metrics, backend userauth.Backend) *Server {
	cfg.SubmissionAddr = cfg.SubmissionTLSAddr
	s := NewSubmissionServer(cfg, q, metrics, backend)
	s.implicitTLS = true
	return s
}

func (s *Server) Run(ctx context.Context) error {
	if s.policyHookClient != nil {
		defer s.policyHookClient.Close()
	}
	if s.policyHookErr != nil {
		return s.policyHookErr
	}
	if s.tlsLoadErr != nil {
		return s.tlsLoadErr
	}
	if s.rateRuleErr != nil {
		return s.rateRuleErr
	}
	if s.rateStoreErr != nil {
		return s.rateStoreErr
	}
	if s.implicitTLS && s.tlsConfig == nil {
		return errors.New("implicit TLS Submission requires a certificate and key")
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
	// Complete implicit TLS before any SMTP reply, including ingress rejections.
	if s.implicitTLS {
		if s.tlsConfig == nil {
			return
		}
		tlsConn := tls.Server(conn, s.tlsConfig)
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return
		}
		conn = tlsConn
		defer tlsConn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	}
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
	ss = &session{remote: conn.RemoteAddr().String(), bodyMode: "7BIT", tls: s.implicitTLS}
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
			ss.acceptGreeting(true)
			methods := s.authMechanisms(ss.tls)
			writeEHLOResponse(w, s.cfg.Hostname, s.cfg.MaxMessageBytes, s.tlsConfig != nil && !ss.tls, len(methods) > 0, methods...)
		case "HELO":
			if arg == "" {
				writeResp(w, 501, "HELO requires domain")
				continue
			}
			ss.helo = arg
			ss.acceptGreeting(false)
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
				ctx,
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
			ss.resetTransaction()
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
			upgraded, err := s.handleStartTLSCommand(sessionCtx, ss, conn, w)
			if err != nil {
				return
			}
			if upgraded != nil {
				conn = upgraded
				r = bufio.NewReader(conn)
				w = bufio.NewWriter(conn)
			}
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
