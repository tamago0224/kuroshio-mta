package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
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

type mailFromArgs struct {
	Address string
	Size    int64
	HasSize bool
	Body    string
}

var (
	errMailParamUnsupported = errors.New("unsupported MAIL parameter")
	errSMTPUTF8Param        = errors.New("unsupported SMTPUTF8 parameter")
	errSMTPUTF8Address      = errors.New("smtputf8 address is not supported")
	errDataLineTooLong      = errors.New("data line too long")
	errMessageTooLarge      = errors.New("message too large")

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
		line, err := r.ReadString('\n')
		if err != nil {
			sessionErr = err
			return
		}
		if len(line) > 512 {
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
			writeEHLOResponse(w, s.cfg.Hostname, s.cfg.MaxMessageBytes, s.tlsConfig != nil && !ss.tls, s.submission)
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
			if !ss.seenHelo {
				cmdSpan.setResponse(503, "send EHLO/HELO first")
				cmdSpan.end()
				writeResp(w, 503, "send EHLO/HELO first")
				continue
			}
			if s.submission && s.cfg.SubmissionAuth && !ss.authOK {
				cmdSpan.reject("auth_required", 530, "authentication required")
				cmdSpan.end()
				writeResp(w, 530, "authentication required")
				continue
			}
			mailArgs, err := parseMailFrom(arg)
			if err != nil {
				cmdSpan.recordError(err)
				if errors.Is(err, errSMTPUTF8Address) {
					cmdSpan.reject("smtputf8_address", 553, err.Error())
					cmdSpan.end()
					writeResp(w, 553, err.Error())
					continue
				}
				if errors.Is(err, errSMTPUTF8Param) {
					cmdSpan.reject("smtputf8_parameter", 555, err.Error())
					cmdSpan.end()
					writeResp(w, 555, err.Error())
					continue
				}
				if errors.Is(err, errMailParamUnsupported) {
					cmdSpan.reject("unsupported_parameter", 555, err.Error())
					cmdSpan.end()
					writeResp(w, 555, err.Error())
					continue
				}
				cmdSpan.setResponse(501, err.Error())
				cmdSpan.end()
				writeResp(w, 501, err.Error())
				continue
			}
			cmdSpan.SetAttributes(
				attribute.String("smtp.mail_from", logging.MaskEmail(mailArgs.Address)),
				attribute.Bool("smtp.mail.has_size", mailArgs.HasSize),
				attribute.String("smtp.mail.body_mode", mailArgs.Body),
			)
			if mailArgs.HasSize {
				cmdSpan.SetAttributes(attribute.Int64("smtp.mail.size", mailArgs.Size))
			}
			if !ss.extended && (mailArgs.HasSize || mailArgs.Body != "") {
				cmdSpan.reject("mail_params_require_ehlo", 555, "MAIL parameters require EHLO")
				cmdSpan.end()
				writeResp(w, 555, "MAIL parameters require EHLO")
				continue
			}
			if mailArgs.HasSize && mailArgs.Size > s.cfg.MaxMessageBytes {
				cmdSpan.reject("message_too_large", 552, "message size exceeds fixed maximum message size")
				cmdSpan.end()
				writeResp(w, 552, "message size exceeds fixed maximum message size")
				continue
			}
			if s.submission && s.cfg.SubmissionSenderID && ss.authOK && !senderAllowedForAuth(ss.auth, mailArgs.Address) {
				scopeMode := submissionSenderScopeMode(ss.auth)
				cmdSpan.SetAttributes(
					attribute.String("smtp.auth.backend", ss.auth.AuthSource),
					attribute.String("smtp.auth.sender_scope_mode", scopeMode),
					attribute.Int("smtp.auth.allowed_sender_domain_count", len(ss.auth.AllowedSenderDomains)),
					attribute.Int("smtp.auth.allowed_sender_address_count", len(ss.auth.AllowedSenderAddresses)),
				)
				slog.WarnContext(ctx, "submission sender identity rejected", "component", "smtp", "reason", "sender_scope_mismatch", "auth_backend", ss.auth.AuthSource, "auth_user", logging.MaskEmail(ss.auth.Username), "mail_from", logging.MaskEmail(mailArgs.Address), "sender_scope_mode", scopeMode)
				cmdSpan.reject("sender_not_allowed_for_auth", 553, "sender address rejected for authenticated identity")
				cmdSpan.end()
				writeResp(w, 553, "sender address rejected for authenticated identity")
				continue
			}
			if remoteIP != nil && s.flexLimiter != nil {
				allowed, err := s.flexLimiter.Allow("mailfrom", remoteIP.String(), ss.helo, mailArgs.Address, time.Now().UTC())
				if err != nil {
					cmdSpan.recordError(err)
					slog.WarnContext(ctx, "rate limit store error", "component", "smtp", "error", err, "remote_ip", remoteIP.String(), "event", "mailfrom", "helo", ss.helo, "mail_from", logging.MaskEmail(mailArgs.Address))
				} else if !allowed {
					cmdSpan.reject("rate_rule_mailfrom", 421, "rate limit exceeded, try again later")
					cmdSpan.end()
					s.metricInc("smtp_reject_rate_limit")
					slog.WarnContext(ctx, "ingress rejected", "component", "smtp", "reason", "rate_rule_mailfrom", "remote_ip", remoteIP.String(), "helo", ss.helo, "mail_from", logging.MaskEmail(mailArgs.Address))
					writeResp(w, 421, "rate limit exceeded, try again later")
					return
				}
			}
			ss.mailFrom = mailArgs.Address
			ss.bodyMode = mailArgs.Body
			if ss.bodyMode == "" {
				ss.bodyMode = "7BIT"
			}
			ss.rcptTo = nil
			ss.data = nil
			cmdSpan.setResponse(250, "sender ok")
			cmdSpan.end()
			writeResp(w, 250, "sender ok")
		case "RCPT":
			_, cmdSpan := startSMTPCommandSpan(sessionCtx, "smtp.rcpt")
			if ss.mailFrom == "" {
				cmdSpan.setResponse(503, "send MAIL FROM first")
				cmdSpan.end()
				writeResp(w, 503, "send MAIL FROM first")
				continue
			}
			addr, err := parseRcptTo(arg, s.cfg.Hostname)
			if err != nil {
				cmdSpan.recordError(err)
				if errors.Is(err, errSMTPUTF8Address) {
					cmdSpan.reject("smtputf8_address", 553, err.Error())
					cmdSpan.end()
					writeResp(w, 553, err.Error())
					continue
				}
				if strings.Contains(strings.ToLower(err.Error()), "parameters") {
					cmdSpan.reject("unsupported_parameters", 555, err.Error())
					cmdSpan.end()
					writeResp(w, 555, err.Error())
				} else {
					cmdSpan.setResponse(501, err.Error())
					cmdSpan.end()
					writeResp(w, 501, err.Error())
				}
				continue
			}
			cmdSpan.SetAttributes(attribute.String("smtp.rcpt_to", logging.MaskEmail(addr)))
			ss.rcptTo = append(ss.rcptTo, addr)
			cmdSpan.setResponse(250, "recipient ok")
			cmdSpan.end()
			writeResp(w, 250, "recipient ok")
		case "DATA":
			_, cmdSpan := startSMTPCommandSpan(
				sessionCtx,
				"smtp.data",
				attribute.Int("smtp.rcpt_count", len(ss.rcptTo)),
				attribute.String("smtp.body_mode", ss.bodyMode),
				attribute.String("smtp.mail_from", logging.MaskEmail(ss.mailFrom)),
			)
			if len(ss.rcptTo) == 0 {
				cmdSpan.setResponse(503, "need RCPT TO before DATA")
				cmdSpan.end()
				writeResp(w, 503, "need RCPT TO before DATA")
				continue
			}
			cmdSpan.addEvent("smtp.data.ready")
			writeResp(w, 354, "end with <CRLF>.<CRLF>")
			data, err := readData(r, s.cfg.MaxMessageBytes)
			if err != nil {
				cmdSpan.recordError(err)
				switch {
				case errors.Is(err, errDataLineTooLong):
					cmdSpan.reject("data_line_too_long", 500, "line too long")
					cmdSpan.end()
					writeResp(w, 500, "line too long")
				case errors.Is(err, errMessageTooLarge):
					cmdSpan.reject("message_too_large", 552, "message size exceeds fixed maximum message size")
					cmdSpan.end()
					writeResp(w, 552, "message size exceeds fixed maximum message size")
				default:
					cmdSpan.setResponse(451, "temporary local problem")
					cmdSpan.end()
					writeResp(w, 451, "temporary local problem")
				}
				continue
			}
			cmdSpan.SetAttributes(attribute.Int("smtp.data.bytes", len(data)))
			if !strings.EqualFold(ss.bodyMode, "8BITMIME") && contains8Bit(data) {
				cmdSpan.reject("body_requires_8bitmime", 554, "8-bit data is not permitted without BODY=8BITMIME")
				cmdSpan.end()
				writeResp(w, 554, "8-bit data is not permitted without BODY=8BITMIME")
				ss.mailFrom = ""
				ss.rcptTo = nil
				ss.data = nil
				continue
			}
			ss.data = data
			msgRemoteIP := remoteIP
			if msgRemoteIP == nil {
				msgRemoteIP = parseRemoteIP(ss.remote)
			}
			authRes := evaluateAuthWithPolicy(msgRemoteIP, ss.helo, ss.mailFrom, ss.data, mailauth.SPFPolicy{
				HeloMode:       s.cfg.SPFHeloPolicy,
				MailFromMode:   s.cfg.SPFMailFromPolicy,
				ARCFailureMode: s.cfg.ARCFailurePolicy,
			})
			s.metricAuthResult(authRes)
			cmdSpan.SetAttributes(
				attribute.String("smtp.auth.action", string(authRes.Action)),
				attribute.String("smtp.auth.dmarc.result", authRes.DMARC.Result),
				attribute.String("smtp.auth.dmarc.policy", authRes.DMARC.Policy),
			)
			switch authRes.Action {
			case mailauth.ActionReject:
				cmdSpan.reject("auth_policy_reject", 550, "message rejected by auth policy")
				cmdSpan.end()
				writeResp(w, 550, "message rejected by auth policy")
				ss.mailFrom = ""
				ss.rcptTo = nil
				ss.data = nil
				continue
			case mailauth.ActionQuarantine:
				ar := mailauth.BuildAuthResultsHeader(s.cfg.Hostname, authRes, ss.mailFrom)
				ss.data = mailauth.InjectHeaders(ss.data, []string{ar, "X-Kuroshio-Quarantine: true"})
			default:
				ar := mailauth.BuildAuthResultsHeader(s.cfg.Hostname, authRes, ss.mailFrom)
				ss.data = mailauth.InjectHeaders(ss.data, []string{ar})
			}
			id, err := newID()
			if err != nil {
				cmdSpan.recordError(err)
				cmdSpan.setResponse(451, "temporary local problem")
				cmdSpan.end()
				writeResp(w, 451, "temporary local problem")
				continue
			}
			received := buildReceivedHeader(s.cfg.Hostname, ss.helo, ss.remote, id, time.Now().UTC(), ss.extended, ss.tls)
			ss.data = mailauth.InjectHeaders(ss.data, []string{received})

			if err := s.enqueue(ctx, ss, id); err != nil {
				cmdSpan.recordError(err)
				cmdSpan.SetAttributes(attribute.String("smtp.message_id", id))
				cmdSpan.setResponse(451, "temporary local problem")
				cmdSpan.end()
				slog.Error("enqueue failed", "component", "smtp", "error", err, "msg_id", id, "remote_ip", ipString(msgRemoteIP))
				s.metricInc("smtp_enqueue_fail")
				writeResp(w, 451, "temporary local problem")
				continue
			}
			s.enqueueDMARCReports(ctx, authRes, ss.mailFrom, id, time.Now().UTC())
			s.metricInc("smtp_queued_messages")
			cmdSpan.SetAttributes(attribute.String("smtp.message_id", id))
			cmdSpan.setResponse(250, "queued")
			cmdSpan.end()
			writeResp(w, 250, "queued")
			ss.mailFrom = ""
			ss.rcptTo = nil
			ss.data = nil
		case "RSET":
			ss.mailFrom = ""
			ss.bodyMode = "7BIT"
			ss.rcptTo = nil
			ss.data = nil
			writeResp(w, 250, "reset state")
		case "AUTH":
			mech, _ := authMechanism(arg)
			_, cmdSpan := startSMTPCommandSpan(sessionCtx, "smtp.auth", attribute.String("smtp.auth.mechanism", mech))
			if !s.submission {
				cmdSpan.setResponse(502, "AUTH is not supported")
				cmdSpan.end()
				writeResp(w, 502, "AUTH is not supported")
				continue
			}
			if !ss.extended {
				cmdSpan.setResponse(503, "send EHLO first")
				cmdSpan.end()
				writeResp(w, 503, "send EHLO first")
				continue
			}
			if s.authBackend == nil {
				cmdSpan.setResponse(454, "authentication backend unavailable")
				cmdSpan.end()
				writeResp(w, 454, "authentication backend unavailable")
				continue
			}
			principal, ok, err := s.handleAuth(r, w, arg)
			if err != nil {
				cmdSpan.recordError(err)
				cmdSpan.setResponse(501, err.Error())
				cmdSpan.end()
				writeResp(w, 501, err.Error())
				continue
			}
			if !ok {
				cmdSpan.SetAttributes(
					attribute.Bool("smtp.auth.success", false),
					attribute.String("smtp.auth.user", logging.MaskEmail(principal.Username)),
				)
				cmdSpan.reject("invalid_credentials", 535, "authentication credentials invalid")
				cmdSpan.end()
				writeResp(w, 535, "authentication credentials invalid")
				continue
			}
			ss.authUser = principal.Username
			ss.auth = principal
			ss.authOK = true
			cmdSpan.SetAttributes(
				attribute.Bool("smtp.auth.success", true),
				attribute.String("smtp.auth.backend", principal.AuthSource),
				attribute.String("smtp.auth.user", logging.MaskEmail(principal.Username)),
				attribute.String("smtp.auth.sender_scope_mode", submissionSenderScopeMode(principal)),
				attribute.Int("smtp.auth.allowed_sender_domain_count", len(principal.AllowedSenderDomains)),
				attribute.Int("smtp.auth.allowed_sender_address_count", len(principal.AllowedSenderAddresses)),
			)
			cmdSpan.setResponse(235, "authentication successful")
			cmdSpan.end()
			writeResp(w, 235, "authentication successful")
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

func (s *Server) enqueue(ctx context.Context, ss *session, id string) error {
	msg := &model.Message{
		ID:         id,
		RemoteAddr: ss.remote,
		Helo:       ss.helo,
		MailFrom:   ss.mailFrom,
		RcptTo:     append([]string(nil), ss.rcptTo...),
		Data:       append([]byte(nil), ss.data...),
	}
	observability.InjectTraceContext(ctx, msg)
	return s.queue.Enqueue(msg)
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

func parseMailFrom(arg string) (mailFromArgs, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		return mailFromArgs{}, errors.New("MAIL must be MAIL FROM:<addr>")
	}
	path, params, err := splitPathAndParams(strings.TrimSpace(arg[5:]))
	if err != nil {
		return mailFromArgs{}, err
	}
	addr, err := util.NormalizePath(path)
	if err != nil {
		return mailFromArgs{}, err
	}
	if addr != "" && !isASCII(addr) {
		return mailFromArgs{}, errSMTPUTF8Address
	}
	out := mailFromArgs{Address: addr}
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(p), "SMTPUTF8") {
			return mailFromArgs{}, fmt.Errorf("%w: SMTPUTF8", errSMTPUTF8Param)
		}
		key, val, ok := splitParamKV(p)
		if !ok {
			return mailFromArgs{}, fmt.Errorf("invalid MAIL parameter: %s", p)
		}
		switch key {
		case "SIZE":
			n, pErr := strconv.ParseInt(val, 10, 64)
			if pErr != nil || n < 0 {
				return mailFromArgs{}, errors.New("invalid SIZE parameter")
			}
			out.HasSize = true
			out.Size = n
		case "BODY":
			v := strings.ToUpper(val)
			if v != "7BIT" && v != "8BITMIME" {
				return mailFromArgs{}, errors.New("invalid BODY parameter")
			}
			out.Body = v
		default:
			return mailFromArgs{}, fmt.Errorf("%w: %s", errMailParamUnsupported, key)
		}
	}
	return out, nil
}

func parseRcptTo(arg string, hostname string) (string, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return "", errors.New("RCPT must be RCPT TO:<addr>")
	}
	path, params, err := splitPathAndParams(strings.TrimSpace(arg[3:]))
	if err != nil {
		return "", err
	}
	if len(params) > 0 {
		return "", errors.New("RCPT parameters are not supported")
	}
	unwrapped := unwrapPath(path)
	if strings.EqualFold(unwrapped, "postmaster") {
		host := strings.ToLower(strings.TrimSpace(hostname))
		if host == "" {
			host = "localhost"
		}
		return "postmaster@" + host, nil
	}
	addr, err := util.NormalizePath(path)
	if err != nil {
		return "", err
	}
	if addr == "" {
		return "", errors.New("recipient is empty")
	}
	if !isASCII(addr) {
		return "", errSMTPUTF8Address
	}
	return addr, nil
}

func readData(r *bufio.Reader, maxBytes int64) ([]byte, error) {
	var out []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) > 1000 {
			return nil, errDataLineTooLong
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		out = append(out, []byte(line)...)
		out = append(out, '\r', '\n')
		if int64(len(out)) > maxBytes {
			return nil, errMessageTooLarge
		}
	}
	return out, nil
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

func writeEHLOResponse(w *bufio.Writer, hostname string, maxMessageBytes int64, advertiseStartTLS bool, advertiseAuth bool) {
	_ = writeLine(w, "250-"+hostname)
	_ = writeLine(w, "250-PIPELINING")
	_ = writeLine(w, fmt.Sprintf("250-SIZE %d", maxMessageBytes))
	_ = writeLine(w, "250-8BITMIME")
	if advertiseAuth {
		_ = writeLine(w, "250-AUTH PLAIN LOGIN")
	}
	if advertiseStartTLS {
		_ = writeLine(w, "250-STARTTLS")
	}
	_ = writeLine(w, "250 HELP")
}

func splitPathAndParams(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, errors.New("missing path")
	}
	if raw[0] == '<' {
		end := strings.IndexByte(raw, '>')
		if end < 0 {
			return "", nil, errors.New("path must be enclosed with <>")
		}
		path := strings.TrimSpace(raw[:end+1])
		rest := strings.TrimSpace(raw[end+1:])
		if rest == "" {
			return path, nil, nil
		}
		return path, strings.Fields(rest), nil
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", nil, errors.New("missing path")
	}
	if len(parts) == 1 {
		return parts[0], nil, nil
	}
	return parts[0], parts[1:], nil
}

func splitParamKV(v string) (key, value string, ok bool) {
	idx := strings.IndexByte(v, '=')
	if idx <= 0 || idx == len(v)-1 {
		return "", "", false
	}
	return strings.ToUpper(strings.TrimSpace(v[:idx])), strings.TrimSpace(v[idx+1:]), true
}

func unwrapPath(path string) string {
	s := strings.TrimSpace(path)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) >= 2 {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
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

func contains8Bit(data []byte) bool {
	for _, b := range data {
		if b >= 0x80 {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) handleAuth(r *bufio.Reader, w *bufio.Writer, arg string) (userauth.Principal, bool, error) {
	if strings.TrimSpace(arg) == "" {
		return userauth.Principal{}, false, errors.New("AUTH requires mechanism")
	}
	parts := strings.Fields(arg)
	mech := strings.ToUpper(parts[0])
	switch mech {
	case "PLAIN":
		var payload string
		if len(parts) >= 2 {
			payload = parts[1]
		} else {
			if err := writeLine(w, "334 "); err != nil {
				return userauth.Principal{}, false, err
			}
			line, err := r.ReadString('\n')
			if err != nil {
				return userauth.Principal{}, false, err
			}
			payload = strings.TrimSpace(line)
		}
		user, pass, err := decodeAuthPlain(payload)
		if err != nil {
			return userauth.Principal{}, false, err
		}
		principal, ok := s.authBackend.AuthenticatePassword(user, pass)
		if !ok {
			return userauth.Principal{Username: user}, false, nil
		}
		return principal, true, nil
	case "LOGIN":
		var userRaw []byte
		if len(parts) >= 2 {
			var err error
			userRaw, err = decodeBase64Line(parts[1])
			if err != nil {
				return userauth.Principal{}, false, errors.New("invalid base64 username")
			}
		} else {
			if err := writeLine(w, "334 VXNlcm5hbWU6"); err != nil {
				return userauth.Principal{}, false, err
			}
			userLine, err := r.ReadString('\n')
			if err != nil {
				return userauth.Principal{}, false, err
			}
			userRaw, err = decodeBase64Line(userLine)
			if err != nil {
				return userauth.Principal{}, false, errors.New("invalid base64 username")
			}
		}
		if err := writeLine(w, "334 UGFzc3dvcmQ6"); err != nil {
			return userauth.Principal{}, false, err
		}
		passLine, err := r.ReadString('\n')
		if err != nil {
			return userauth.Principal{}, false, err
		}
		passRaw, err := decodeBase64Line(passLine)
		if err != nil {
			return userauth.Principal{}, false, errors.New("invalid base64 password")
		}
		user := strings.TrimSpace(string(userRaw))
		pass := strings.TrimSpace(string(passRaw))
		if user == "" || pass == "" {
			return userauth.Principal{}, false, errors.New("empty credentials")
		}
		principal, ok := s.authBackend.AuthenticatePassword(user, pass)
		if !ok {
			return userauth.Principal{Username: user}, false, nil
		}
		return principal, true, nil
	default:
		return userauth.Principal{}, false, errors.New("unsupported auth mechanism")
	}
}

func decodeAuthPlain(payload string) (string, string, error) {
	raw, err := decodeBase64Line(payload)
	if err != nil {
		return "", "", errors.New("invalid base64 credentials")
	}
	parts := bytes.Split(raw, []byte{0})
	if len(parts) < 3 {
		return "", "", errors.New("invalid plain auth payload")
	}
	user := strings.TrimSpace(string(parts[1]))
	pass := strings.TrimSpace(string(parts[2]))
	if user == "" || pass == "" {
		return "", "", errors.New("empty credentials")
	}
	return user, pass, nil
}

func decodeBase64Line(v string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(v))
}

func authMechanism(arg string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(arg))
	if len(parts) == 0 {
		return "", false
	}
	return strings.ToUpper(parts[0]), true
}

func senderAllowedForAuth(principal userauth.Principal, mailFrom string) bool {
	mailFrom = strings.ToLower(strings.TrimSpace(mailFrom))
	if mailFrom == "" {
		return false
	}
	for _, allowed := range principal.AllowedSenderAddresses {
		if strings.EqualFold(strings.TrimSpace(allowed), mailFrom) {
			return true
		}
	}
	if fromDomain, ok := util.DomainOf(mailFrom); ok {
		for _, allowed := range principal.AllowedSenderDomains {
			if strings.EqualFold(strings.TrimSpace(allowed), fromDomain) {
				return true
			}
		}
	}
	authDomain, ok := util.DomainOf(strings.ToLower(strings.TrimSpace(principal.Username)))
	if !ok {
		return false
	}
	fromDomain, ok := util.DomainOf(mailFrom)
	if !ok {
		return false
	}
	return strings.EqualFold(authDomain, fromDomain)
}

func submissionSenderScopeMode(principal userauth.Principal) string {
	if len(principal.AllowedSenderDomains) > 0 || len(principal.AllowedSenderAddresses) > 0 {
		return "credential_scope"
	}
	return "username_domain_fallback"
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
