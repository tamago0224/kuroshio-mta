package smtp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/util"
	"go.opentelemetry.io/otel/attribute"
)

type mailFromArgs struct {
	Address    string
	Size       int64
	HasSize    bool
	Body       string
	DSN        *model.DSN
	RequireTLS bool
}

type rcptToArgs struct {
	Address string
	DSN     model.DSNRecipientOptions
}

var (
	errMailParamUnsupported = errors.New("unsupported MAIL parameter")
	errRcptParamUnsupported = errors.New("unsupported RCPT parameter")
	errSMTPUTF8Param        = errors.New("unsupported SMTPUTF8 parameter")
	errSMTPUTF8Address      = errors.New("smtputf8 address is not supported")
)

// handleMailCommand reports whether rate limiting requires closing the connection.
func (s *Server) handleMailCommand(
	ctx context.Context,
	ss *session,
	remoteIP net.IP,
	w *bufio.Writer,
	arg string,
	cmdSpan *smtpCommandSpan,
) (closeConnection bool) {
	defer cmdSpan.end()
	if !ss.seenHelo {
		cmdSpan.setResponse(503, "send EHLO/HELO first")
		writeResp(w, 503, "send EHLO/HELO first")
		return false
	}
	if s.submission && s.cfg.SubmissionAuth && !ss.authOK {
		cmdSpan.reject("auth_required", 530, "authentication required")
		writeResp(w, 530, "authentication required")
		return false
	}
	mailArgs, err := parseMailFrom(arg)
	if err != nil {
		cmdSpan.recordError(err)
		if errors.Is(err, errSMTPUTF8Address) {
			cmdSpan.reject("smtputf8_address", 553, err.Error())
			writeResp(w, 553, err.Error())
			return false
		}
		if errors.Is(err, errSMTPUTF8Param) {
			cmdSpan.reject("smtputf8_parameter", 555, err.Error())
			writeResp(w, 555, err.Error())
			return false
		}
		if errors.Is(err, errMailParamUnsupported) {
			cmdSpan.reject("unsupported_parameter", 555, err.Error())
			writeResp(w, 555, err.Error())
			return false
		}
		cmdSpan.setResponse(501, err.Error())
		writeResp(w, 501, err.Error())
		return false
	}
	cmdSpan.SetAttributes(
		attribute.String("smtp.mail_from", logging.MaskEmail(mailArgs.Address)),
		attribute.Bool("smtp.mail.has_size", mailArgs.HasSize),
		attribute.String("smtp.mail.body_mode", mailArgs.Body),
	)
	if mailArgs.HasSize {
		cmdSpan.SetAttributes(attribute.Int64("smtp.mail.size", mailArgs.Size))
	}
	if !ss.extended && (mailArgs.HasSize || mailArgs.Body != "" || mailArgs.DSN != nil || mailArgs.RequireTLS) {
		cmdSpan.reject("mail_params_require_ehlo", 555, "MAIL parameters require EHLO")
		writeResp(w, 555, "MAIL parameters require EHLO")
		return false
	}
	if mailArgs.RequireTLS && !s.requireTLSAvailable() {
		cmdSpan.reject("requiretls_unavailable", 555, "REQUIRETLS is not available on this listener")
		writeResp(w, 555, "REQUIRETLS is not available on this listener")
		return false
	}
	if mailArgs.HasSize && mailArgs.Size > s.cfg.MaxMessageBytes {
		cmdSpan.reject("message_too_large", 552, "message size exceeds fixed maximum message size")
		writeResp(w, 552, "message size exceeds fixed maximum message size")
		return false
	}
	enforceSender := s.submission && s.cfg.SubmissionSenderID && ss.authOK
	if enforceSender && !senderAllowedForAuth(ss.auth, mailArgs.Address) {
		scopeMode := submissionSenderScopeMode(ss.auth)
		cmdSpan.SetAttributes(
			attribute.String("smtp.auth.backend", ss.auth.AuthSource),
			attribute.String("smtp.auth.sender_scope_mode", scopeMode),
			attribute.Int("smtp.auth.allowed_sender_domain_count", len(ss.auth.AllowedSenderDomains)),
			attribute.Int("smtp.auth.allowed_sender_address_count", len(ss.auth.AllowedSenderAddresses)),
		)
		slog.WarnContext(
			ctx,
			"submission sender identity rejected",
			"component", "smtp",
			"reason", "sender_scope_mismatch",
			"auth_backend", ss.auth.AuthSource,
			"auth_user", logging.MaskEmail(ss.auth.Username),
			"mail_from", logging.MaskEmail(mailArgs.Address),
			"sender_scope_mode", scopeMode,
		)
		cmdSpan.reject("sender_not_allowed_for_auth", 553, "sender address rejected for authenticated identity")
		writeResp(w, 553, "sender address rejected for authenticated identity")
		return false
	}
	if remoteIP != nil && s.flexLimiter != nil {
		allowed, err := s.flexLimiter.Allow(
			"mailfrom",
			remoteIP.String(),
			ss.helo,
			mailArgs.Address,
			time.Now().UTC(),
		)
		if err != nil {
			cmdSpan.recordError(err)
			slog.WarnContext(
				ctx,
				"rate limit store error",
				"component", "smtp",
				"error", err,
				"remote_ip", remoteIP.String(),
				"event", "mailfrom",
				"helo", ss.helo,
				"mail_from", logging.MaskEmail(mailArgs.Address),
			)
		} else if !allowed {
			cmdSpan.reject("rate_rule_mailfrom", 421, "rate limit exceeded, try again later")
			s.metricInc("smtp_reject_rate_limit")
			slog.WarnContext(
				ctx,
				"ingress rejected",
				"component", "smtp",
				"reason", "rate_rule_mailfrom",
				"remote_ip", remoteIP.String(),
				"helo", ss.helo,
				"mail_from", logging.MaskEmail(mailArgs.Address),
			)
			writeResp(w, 421, "rate limit exceeded, try again later")
			return true
		}
	}
	ss.beginTransaction(mailArgs)
	cmdSpan.setResponse(250, "sender ok")
	writeResp(w, 250, "sender ok")
	return false
}

func (s *Server) handleRcptCommand(
	ctx context.Context,
	ss *session,
	w *bufio.Writer,
	arg string,
	cmdSpan *smtpCommandSpan,
) {
	defer cmdSpan.end()
	if ss.mailFrom == "" {
		cmdSpan.setResponse(503, "send MAIL FROM first")
		writeResp(w, 503, "send MAIL FROM first")
		return
	}
	if len(ss.rcptTo) >= s.cfg.SMTPLimits.WithDefaults().MaxRecipients {
		s.metricInc("smtp_limit_recipients_total")
		cmdSpan.reject("recipient_limit", 452, "4.5.3 too many recipients")
		writeResp(w, 452, "4.5.3 too many recipients")
		return
	}
	rcptArgs, err := parseRcptToArgs(arg, s.cfg.Hostname)
	if err != nil {
		cmdSpan.recordError(err)
		if errors.Is(err, errSMTPUTF8Address) {
			cmdSpan.reject("smtputf8_address", 553, err.Error())
			writeResp(w, 553, err.Error())
			return
		}
		if errors.Is(err, errRcptParamUnsupported) {
			cmdSpan.reject("unsupported_parameters", 555, err.Error())
			writeResp(w, 555, err.Error())
			return
		}
		cmdSpan.setResponse(501, err.Error())
		writeResp(w, 501, err.Error())
		return
	}
	if !ss.extended && (len(rcptArgs.DSN.Notify) > 0 || rcptArgs.DSN.OriginalRecipient != "") {
		cmdSpan.reject("rcpt_params_require_ehlo", 555, "RCPT parameters require EHLO")
		writeResp(w, 555, "RCPT parameters require EHLO")
		return
	}
	addr := rcptArgs.Address
	cmdSpan.SetAttributes(attribute.String("smtp.rcpt_to", logging.MaskEmail(addr)))
	policy := s.recipientPolicy
	if policy == nil {
		policy, err = newRecipientPolicy(s.cfg.InboundPolicy)
	}
	reason := ""
	if err == nil {
		reason, err = policy.Authorize(ctx, parseRemoteIP(ss.remote), s.submission && ss.authOK, addr)
	}
	if err != nil {
		cmdSpan.reject("recipient_backend_unavailable", 451, "recipient validation temporarily unavailable")
		s.metricInc("smtp_reject_recipient_backend_unavailable")
		writeResp(w, 451, "recipient validation temporarily unavailable")
		return
	}
	if reason != "" {
		code, text := 550, "5.7.1 relaying denied"
		if reason == "unknown_recipient" {
			text = "5.1.1 unknown recipient"
		}
		cmdSpan.reject(reason, code, text)
		s.metricInc("smtp_reject_" + reason)
		writeResp(w, code, text)
		return
	}
	if !s.applyPolicyHook(ctx, ss, "rcpt", addr, w, cmdSpan) {
		return
	}
	ss.rcptTo = append(ss.rcptTo, addr)
	if len(rcptArgs.DSN.Notify) > 0 || rcptArgs.DSN.OriginalRecipient != "" {
		if ss.dsn == nil {
			ss.dsn = &model.DSN{}
		}
		if ss.dsn.Recipients == nil {
			ss.dsn.Recipients = make(map[string]model.DSNRecipientOptions)
		}
		ss.dsn.Recipients[addr] = rcptArgs.DSN
	}
	cmdSpan.setResponse(250, "recipient ok")
	writeResp(w, 250, "recipient ok")
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
		if strings.EqualFold(strings.TrimSpace(p), "REQUIRETLS") {
			if out.RequireTLS {
				return mailFromArgs{}, errors.New("duplicate REQUIRETLS parameter")
			}
			out.RequireTLS = true
			continue
		}
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
		case "RET":
			if out.DSN != nil && out.DSN.Return != "" {
				return mailFromArgs{}, errors.New("duplicate RET parameter")
			}
			v := strings.ToUpper(val)
			if v != "FULL" && v != "HDRS" {
				return mailFromArgs{}, errors.New("invalid RET parameter")
			}
			if out.DSN == nil {
				out.DSN = &model.DSN{}
			}
			out.DSN.Return = v
		case "ENVID":
			if out.DSN != nil && out.DSN.EnvelopeID != "" {
				return mailFromArgs{}, errors.New("duplicate ENVID parameter")
			}
			if err := validateXText(val, 100); err != nil {
				return mailFromArgs{}, fmt.Errorf("invalid ENVID parameter: %w", err)
			}
			if out.DSN == nil {
				out.DSN = &model.DSN{}
			}
			out.DSN.EnvelopeID = val
		default:
			return mailFromArgs{}, fmt.Errorf("%w: %s", errMailParamUnsupported, key)
		}
	}
	return out, nil
}

func (s *Server) requireTLSAvailable() bool {
	if !s.submission || !s.cfg.SubmissionAuth {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(s.cfg.DeliveryMode))
	if mode != "mx" && mode != "relay" {
		return false
	}
	for _, route := range s.cfg.DeliveryRoutes {
		routeMode := strings.ToLower(strings.TrimSpace(route.Mode))
		if routeMode != "mx" && routeMode != "relay" {
			return false
		}
	}
	return true
}

func parseRcptTo(arg string, hostname string) (string, error) {
	parsed, err := parseRcptToArgs(arg, hostname)
	return parsed.Address, err
}

func parseRcptToArgs(arg string, hostname string) (rcptToArgs, error) {
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return rcptToArgs{}, errors.New("RCPT must be RCPT TO:<addr>")
	}
	path, params, err := splitPathAndParams(strings.TrimSpace(arg[3:]))
	if err != nil {
		return rcptToArgs{}, err
	}
	unwrapped := unwrapPath(path)
	var addr string
	if strings.EqualFold(unwrapped, "postmaster") {
		host := strings.ToLower(strings.TrimSpace(hostname))
		if host == "" {
			host = "localhost"
		}
		addr = "postmaster@" + host
	} else {
		addr, err = util.NormalizePath(path)
		if err != nil {
			return rcptToArgs{}, err
		}
	}
	if addr == "" {
		return rcptToArgs{}, errors.New("recipient is empty")
	}
	if !isASCII(addr) {
		return rcptToArgs{}, errSMTPUTF8Address
	}
	out := rcptToArgs{Address: addr}
	seen := make(map[string]bool, len(params))
	for _, p := range params {
		key, val, ok := splitParamKV(p)
		if !ok {
			return rcptToArgs{}, fmt.Errorf("invalid RCPT parameter: %s", p)
		}
		if seen[key] {
			return rcptToArgs{}, fmt.Errorf("duplicate RCPT parameter: %s", key)
		}
		seen[key] = true
		switch key {
		case "NOTIFY":
			notify, err := parseNotify(val)
			if err != nil {
				return rcptToArgs{}, err
			}
			out.DSN.Notify = notify
		case "ORCPT":
			parts := strings.SplitN(val, ";", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				return rcptToArgs{}, errors.New("invalid ORCPT parameter")
			}
			if err := validateXText(parts[1], 500); err != nil {
				return rcptToArgs{}, fmt.Errorf("invalid ORCPT parameter: %w", err)
			}
			out.DSN.OriginalRecipient = strings.ToLower(parts[0]) + ";" + parts[1]
		default:
			return rcptToArgs{}, fmt.Errorf("%w: %s", errRcptParamUnsupported, key)
		}
	}
	return out, nil
}

func parseNotify(value string) ([]string, error) {
	parts := strings.Split(strings.ToUpper(value), ",")
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		if part != "SUCCESS" && part != "FAILURE" && part != "DELAY" && part != "NEVER" {
			return nil, errors.New("invalid NOTIFY parameter")
		}
		if seen[part] {
			return nil, errors.New("duplicate NOTIFY value")
		}
		seen[part] = true
	}
	if seen["NEVER"] && len(parts) != 1 {
		return nil, errors.New("NOTIFY NEVER cannot be combined")
	}
	return parts, nil
}

func validateXText(value string, maxLen int) error {
	if value == "" || len(value) > maxLen {
		return errors.New("xtext length is invalid")
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b == '+' {
			if i+2 >= len(value) || !isHex(value[i+1]) || !isHex(value[i+2]) {
				return errors.New("malformed xtext escape")
			}
			i += 2
			continue
		}
		if b < '!' || b > '~' || b == '=' {
			return errors.New("invalid xtext character")
		}
	}
	return nil
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'F' || b >= 'a' && b <= 'f'
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

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}
