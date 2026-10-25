package smtp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"go.opentelemetry.io/otel/attribute"
)

// handleAuthCommand handles AUTH replies and session state. Only framing errors
// escape to the connection loop, which must close an unsynchronized connection.
func (s *Server) handleAuthCommand(
	ctx context.Context,
	sessionCtx context.Context,
	ss *session,
	r *bufio.Reader,
	w *bufio.Writer,
	arg string,
) error {
	mech, _ := authMechanism(arg)
	if mech != "PLAIN" && mech != "LOGIN" && mech != "XOAUTH2" {
		mech = "UNKNOWN"
	}
	authCtx, cmdSpan := startSMTPCommandSpan(
		sessionCtx,
		"smtp.auth",
		attribute.String("smtp.auth.mechanism", mech),
	)
	defer cmdSpan.end()
	if !s.submission {
		cmdSpan.setResponse(502, "AUTH is not supported")
		writeResp(w, 502, "AUTH is not supported")
		return nil
	}
	if !ss.extended {
		cmdSpan.setResponse(503, "send EHLO first")
		writeResp(w, 503, "send EHLO first")
		return nil
	}
	if s.authBackend == nil {
		cmdSpan.setResponse(454, "authentication backend unavailable")
		writeResp(w, 454, "authentication backend unavailable")
		return nil
	}
	if !ss.tls && (mech == "XOAUTH2" || ((mech == "PLAIN" || mech == "LOGIN") && !s.cfg.SubmissionAllowInsecureAuth)) {
		cmdSpan.setResponse(538, "encryption required for "+mech)
		writeResp(w, 538, "encryption required for "+mech)
		return nil
	}
	authRes, err := s.handleAuth(authCtx, r, w, arg)
	if err != nil {
		if errors.Is(err, errSASLFraming) {
			cmdSpan.recordError(err)
			return err
		}
		cmdSpan.recordError(err)
		ss.authFailures++
		cmdSpan.setResponse(501, err.Error())
		writeResp(w, 501, err.Error())
		return nil
	}
	if !authRes.Success {
		ss.authFailures++
		cmdSpan.SetAttributes(
			attribute.Bool("smtp.auth.success", false),
			attribute.String("smtp.auth.backend", authRes.Principal.AuthSource),
			attribute.String("smtp.auth.user", logging.MaskEmail(authRes.Principal.Username)),
			attribute.String("smtp.auth.failure_reason", authRes.FailureReason),
		)
		slog.WarnContext(
			ctx, "submission auth failed",
			"component", "smtp",
			"auth_backend", authRes.Principal.AuthSource,
			"auth_user", logging.MaskEmail(authRes.Principal.Username),
			"mechanism", mech,
			"failure_reason", authRes.FailureReason,
		)
		cmdSpan.reject("invalid_credentials", 535, "authentication credentials invalid")
		writeResp(w, 535, "authentication credentials invalid")
		return nil
	}
	principal := authRes.Principal
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
	slog.InfoContext(
		ctx, "submission auth succeeded",
		"component", "smtp",
		"auth_backend", principal.AuthSource,
		"auth_user", logging.MaskEmail(principal.Username),
		"mechanism", mech,
		"sender_scope_mode", submissionSenderScopeMode(principal),
		"allowed_sender_domain_count", len(principal.AllowedSenderDomains),
		"allowed_sender_address_count", len(principal.AllowedSenderAddresses),
	)
	cmdSpan.setResponse(235, "authentication successful")
	writeResp(w, 235, "authentication successful")
	return nil
}

func (s *Server) authMechanisms(encrypted bool) []string {
	if !s.submission || s.authBackend == nil {
		return nil
	}
	methods := []string{"PLAIN", "LOGIN"}
	if b, ok := s.authBackend.(userauth.MechanismBackend); ok {
		methods = b.AuthMechanisms()
	}
	result := make([]string, 0, len(methods))
	for _, method := range methods {
		if encrypted || (s.cfg.SubmissionAllowInsecureAuth && (method == "PLAIN" || method == "LOGIN")) {
			result = append(result, method)
		}
	}
	return result
}

func (s *Server) handleAuth(ctx context.Context, r *bufio.Reader, w *bufio.Writer, arg string) (userauth.AuthResult, error) {
	if strings.TrimSpace(arg) == "" {
		return userauth.AuthResult{}, errors.New("AUTH requires mechanism")
	}
	parts := strings.Fields(arg)
	mech := strings.ToUpper(parts[0])
	if !slices.Contains(s.authMechanisms(true), mech) {
		return userauth.AuthResult{}, errors.New("unsupported auth mechanism")
	}
	switch mech {
	case "XOAUTH2":
		return s.handleXOAuth2(ctx, r, w, parts)
	case "PLAIN":
		return s.handleAuthPlain(r, w, parts)
	case "LOGIN":
		return s.handleAuthLogin(r, w, parts)
	default:
		return userauth.AuthResult{}, errors.New("unsupported auth mechanism")
	}
}

func (s *Server) handleAuthPlain(r *bufio.Reader, w *bufio.Writer, parts []string) (userauth.AuthResult, error) {
	var payload string
	if len(parts) >= 2 {
		payload = parts[1]
	} else {
		if err := writeLine(w, "334 "); err != nil {
			return userauth.AuthResult{}, err
		}
		line, err := r.ReadString('\n')
		if err != nil {
			return userauth.AuthResult{}, err
		}
		payload = strings.TrimSpace(line)
	}
	user, pass, err := decodeAuthPlain(payload)
	if err != nil {
		return userauth.AuthResult{}, err
	}
	return s.authenticatePassword(user, pass), nil
}

func (s *Server) handleAuthLogin(r *bufio.Reader, w *bufio.Writer, parts []string) (userauth.AuthResult, error) {
	var userRaw []byte
	if len(parts) >= 2 {
		var err error
		userRaw, err = decodeBase64Line(parts[1])
		if err != nil {
			return userauth.AuthResult{}, errors.New("invalid base64 username")
		}
	} else {
		if err := writeLine(w, "334 VXNlcm5hbWU6"); err != nil {
			return userauth.AuthResult{}, err
		}
		userLine, err := r.ReadString('\n')
		if err != nil {
			return userauth.AuthResult{}, err
		}
		userRaw, err = decodeBase64Line(userLine)
		if err != nil {
			return userauth.AuthResult{}, errors.New("invalid base64 username")
		}
	}
	if err := writeLine(w, "334 UGFzc3dvcmQ6"); err != nil {
		return userauth.AuthResult{}, err
	}
	passLine, err := r.ReadString('\n')
	if err != nil {
		return userauth.AuthResult{}, err
	}
	passRaw, err := decodeBase64Line(passLine)
	if err != nil {
		return userauth.AuthResult{}, errors.New("invalid base64 password")
	}
	user := strings.TrimSpace(string(userRaw))
	pass := strings.TrimSpace(string(passRaw))
	if user == "" || pass == "" {
		return userauth.AuthResult{}, errors.New("empty credentials")
	}
	return s.authenticatePassword(user, pass), nil
}

func (s *Server) authenticatePassword(user, pass string) userauth.AuthResult {
	if detailed, ok := s.authBackend.(userauth.DetailedBackend); ok {
		return detailed.AuthenticatePasswordDetailed(user, pass)
	}
	principal, ok := s.authBackend.AuthenticatePassword(user, pass)
	return userauth.AuthResult{Principal: principal, Success: ok}
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
	return userauth.SenderAllowed(principal, mailFrom)
}

func submissionSenderScopeMode(principal userauth.Principal) string {
	if len(principal.AllowedSenderDomains) > 0 || len(principal.AllowedSenderAddresses) > 0 {
		return "credential_scope"
	}
	return "username_domain_fallback"
}
