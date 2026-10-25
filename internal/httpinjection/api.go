package httpinjection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dkim"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

const endpoint = "/api/v1/messages"

type bearerBackend interface {
	AuthenticateBearerToken(context.Context, string, string) userauth.AuthResult
}

type API struct {
	cfg       config.Config
	queue     queue.Backend
	password  userauth.Backend
	bearer    bearerBackend
	store     *idempotencyStore
	limiter   *principalLimiter
	enqueueMu sync.Mutex
}

type injectRequest struct {
	EnvelopeFrom string   `json:"envelope_from"`
	Recipients   []string `json:"recipients"`
	RawMessage   string   `json:"raw_message"`
}

type injectResponse struct {
	MessageID string `json:"message_id"`
	Replayed  bool   `json:"replayed"`
}

func New(cfg config.Config, backend queue.Backend, password userauth.Backend, bearer bearerBackend) (*API, error) {
	store, err := openIdempotencyStore(cfg.QueueDir, cfg.HTTPInjection.IdempotencyTTL)
	if err != nil {
		return nil, err
	}
	return &API{
		cfg:      cfg,
		queue:    backend,
		password: password,
		bearer:   bearer,
		store:    store,
		limiter:  newPrincipalLimiter(cfg.HTTPInjection.RateLimitPerMinute),
	}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(endpoint, a.handleInject)
	return mux
}

func (a *API) handleInject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	principal, ok := a.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="kuroshio-submission", Bearer`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.limiter.allow(principal.Username, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key")
		return
	}
	req, err := decodeRequest(r.Body, a.cfg.MaxMessageBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	msg, requestHash, err := a.buildMessage(principal, key, req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	observability.InjectTraceContext(r.Context(), msg)

	// Serialize claim/enqueue/commit so concurrent requests cannot publish twice.
	a.enqueueMu.Lock()
	defer a.enqueueMu.Unlock()
	existing, found, err := a.store.lookup(principal.Username, key, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "idempotency_unavailable")
		return
	}
	if found {
		if existing.RequestHash != requestHash {
			writeError(w, http.StatusConflict, "idempotency_key_reused")
			return
		}
		if existing.Committed {
			writeJSON(w, http.StatusOK, injectResponse{MessageID: existing.MessageID, Replayed: true})
			return
		}
	}
	if err := a.store.prepare(principal.Username, key, requestHash, msg.ID, time.Now().UTC()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "idempotency_unavailable")
		return
	}
	if err := a.queue.Enqueue(msg); err != nil {
		slog.Warn("HTTP injection enqueue failed", "component", "http_injection", "error", err, "msg_id", msg.ID)
		writeError(w, http.StatusServiceUnavailable, "enqueue_failed")
		return
	}
	if err := a.store.commit(principal.Username, key, time.Now().UTC()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "idempotency_unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, injectResponse{MessageID: msg.ID})
}

func (a *API) authenticate(r *http.Request) (userauth.Principal, bool) {
	if username, password, ok := r.BasicAuth(); ok && a.password != nil {
		if detailed, ok := a.password.(userauth.DetailedBackend); ok {
			result := detailed.AuthenticatePasswordDetailed(username, password)
			return result.Principal, result.Success
		}
		return a.password.AuthenticatePassword(username, password)
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") || a.bearer == nil {
		return userauth.Principal{}, false
	}
	username := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Submission-User")))
	result := a.bearer.AuthenticateBearerToken(
		r.Context(),
		username,
		strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")),
	)
	return result.Principal, result.Success
}

func (a *API) buildMessage(principal userauth.Principal, key string, req injectRequest) (*model.Message, string, error) {
	mailFrom, err := util.NormalizePath(req.EnvelopeFrom)
	if err != nil || mailFrom == "" || !isASCII(mailFrom) {
		return nil, "", errors.New("invalid_envelope_from")
	}
	if a.cfg.SubmissionSenderID && !userauth.SenderAllowed(principal, mailFrom) {
		return nil, "", errors.New("sender_not_allowed")
	}
	limits := a.cfg.SMTPLimits.WithDefaults()
	if len(req.Recipients) == 0 || len(req.Recipients) > limits.MaxRecipients {
		return nil, "", errors.New("invalid_recipients")
	}
	recipients := make([]string, 0, len(req.Recipients))
	seen := make(map[string]struct{}, len(req.Recipients))
	for _, raw := range req.Recipients {
		recipient, normalizeErr := util.NormalizePath(raw)
		if normalizeErr != nil || recipient == "" || !isASCII(recipient) {
			return nil, "", errors.New("invalid_recipients")
		}
		if _, duplicate := seen[recipient]; duplicate {
			continue
		}
		seen[recipient] = struct{}{}
		recipients = append(recipients, recipient)
	}
	data, err := normalizeMessage([]byte(req.RawMessage), a.cfg.MaxMessageBytes)
	if err != nil {
		return nil, "", errors.New("invalid_raw_message")
	}
	author, err := dkim.FromAddress(data)
	if err != nil || !userauth.SenderAllowed(principal, author) {
		return nil, "", errors.New("author_not_allowed")
	}
	fromDomain, _ := util.DomainOf(author)
	envelopeDomain, _ := util.DomainOf(mailFrom)
	if fromDomain != envelopeDomain {
		return nil, "", errors.New("author_envelope_domain_mismatch")
	}
	payload := struct {
		EnvelopeFrom string   `json:"envelope_from"`
		Recipients   []string `json:"recipients"`
		RawMessage   []byte   `json:"raw_message"`
	}{EnvelopeFrom: mailFrom, Recipients: recipients, RawMessage: data}
	canonical, _ := json.Marshal(payload)
	hash := sha256.Sum256(canonical)
	scopeHash := sha256.Sum256([]byte(strings.ToLower(principal.Username) + "\x00" + key))
	msg := &model.Message{
		ID:              "http-" + hex.EncodeToString(scopeHash[:16]),
		RemoteAddr:      rfc1918HTTPOrigin,
		Helo:            "http-injection",
		MailFrom:        mailFrom,
		RcptTo:          recipients,
		Data:            data,
		Tenant:          strings.ToLower(strings.TrimSpace(principal.Username)),
		TrafficClass:    "transactional",
		SigningIdentity: &model.SigningIdentity{Domain: fromDomain},
	}
	return msg, hex.EncodeToString(hash[:]), nil
}

const rfc1918HTTPOrigin = "http-injection"

func decodeRequest(body io.Reader, maxMessageBytes int64) (injectRequest, error) {
	limit := maxMessageBytes + 1024*1024
	decoder := json.NewDecoder(io.LimitReader(body, limit+1))
	decoder.DisallowUnknownFields()
	var req injectRequest
	if err := decoder.Decode(&req); err != nil {
		return injectRequest{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return injectRequest{}, errors.New("request must contain one JSON value")
	}
	return req, nil
}

func normalizeMessage(raw []byte, maxBytes int64) ([]byte, error) {
	if len(raw) == 0 || int64(len(raw)) > maxBytes || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errors.New("invalid message size")
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for _, line := range lines {
		if len(line) > 998 {
			return nil, errors.New("message line too long")
		}
	}
	data := []byte(strings.Join(lines, "\r\n"))
	if !bytes.HasSuffix(data, []byte("\r\n")) {
		data = append(data, '\r', '\n')
	}
	if int64(len(data)) > maxBytes || !bytes.Contains(data, []byte("\r\n\r\n")) {
		return nil, errors.New("invalid RFC 5322 message")
	}
	return data, nil
}

func validIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 200 {
		return false
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 0x7f {
			return false
		}
	}
	return true
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Run(ctx context.Context, cfg config.Config, api *API) error {
	if !cfg.HTTPInjection.Enabled {
		return nil
	}
	srv := &http.Server{
		Addr:              cfg.HTTPInjection.ListenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 * 1024,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("HTTP injection API listening", "component", "http_injection", "listen_addr", cfg.HTTPInjection.ListenAddr)
	err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("HTTP injection server: %w", err)
}
