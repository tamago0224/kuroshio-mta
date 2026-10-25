package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/deliveryhistory"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
)

type queueManager interface {
	ListState(state string, limit int) ([]*model.Message, error)
	RequeueFromState(state, id string, now time.Time) (*model.Message, error)
}

type role string

const (
	roleViewer   role = "viewer"
	roleOperator role = "operator"
	roleAdmin    role = "admin"
)

type API struct {
	pauses       deliverypause.Store
	suppressions *bounce.SuppressionStore
	queue        queueManager
	reputation   *reputation.Tracker
	authBackend  AuthBackend
	history      *deliveryhistory.Store
	now          func() time.Time
}

type principalContextKey struct{}

func NewAPI(s *bounce.SuppressionStore, q queueManager, rep *reputation.Tracker, tokenConfig string) *API {
	return NewAPIWithBackend(s, q, rep, NewConfigTokenBackend(tokenConfig))
}

func NewAPIWithBackend(s *bounce.SuppressionStore, q queueManager, rep *reputation.Tracker, authBackend AuthBackend) *API {
	return &API{
		suppressions: s,
		queue:        q,
		reputation:   rep,
		authBackend:  authBackend,
		now:          time.Now,
	}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/delivery/pauses", a.handleDeliveryPauses)
	mux.HandleFunc("/api/v1/delivery/pauses/", a.handleDeliveryResume)
	mux.HandleFunc("/api/v1/delivery/history", a.handleDeliveryHistory)
	mux.HandleFunc("/api/v1/delivery/diagnostics", a.handleDeliveryDiagnostics)
	mux.HandleFunc("/api/v1/delivery/diagnostics/", a.handleDeliveryDiagnostic)
	mux.HandleFunc("/api/v1/suppressions", a.handleSuppressions)
	mux.HandleFunc("/api/v1/suppressions/", a.handleSuppressionByAddress)
	mux.HandleFunc("/api/v1/queue/", a.handleQueue)
	mux.HandleFunc("/api/v1/reputation/complaints", a.handleComplaint)
	mux.HandleFunc("/api/v1/reputation/tlsrpt", a.handleTLSReport)
	mux.HandleFunc("/api/v1/reputation/reports/complaints", a.handleReport)
	mux.HandleFunc("/api/v1/reputation/reports/tlsrpt", a.handleReport)
	return mux
}

func (a *API) handleDeliveryDiagnostics(w http.ResponseWriter, r *http.Request) {
	if a.history == nil {
		http.Error(w, "delivery diagnostics are unavailable", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if next, _, ok := a.authorize(w, r, roleViewer); !ok {
			return
		} else {
			r = next
		}
		items, err := a.history.ListDiagnostics(r.Context())
		if err != nil {
			http.Error(w, "delivery diagnostics lookup failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		if next, _, ok := a.authorize(w, r, roleOperator); !ok {
			return
		} else {
			r = next
		}
		var req struct {
			MessageID string    `json:"message_id"`
			ExpiresAt time.Time `json:"expires_at"`
			MaxEvents int       `json:"max_events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		item, err := a.history.CreateDiagnostic(r.Context(), req.MessageID, req.ExpiresAt, req.MaxEvents)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.audit(r, "delivery_diagnostic_create", map[string]any{"message_id": req.MessageID, "diagnostic_id": item.ID, "expires_at": item.ExpiresAt, "max_events": item.MaxEvents})
		writeJSON(w, http.StatusCreated, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleDeliveryDiagnostic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minRole := roleOperator
	if r.Method == http.MethodGet {
		minRole = roleViewer
	}
	if next, _, ok := a.authorize(w, r, minRole); !ok {
		return
	} else {
		r = next
	}
	if a.history == nil {
		http.Error(w, "delivery diagnostics are unavailable", http.StatusNotImplemented)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/delivery/diagnostics/"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid diagnostic id", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		item, page, err := a.history.DiagnosticHistory(r.Context(), id)
		if err != nil {
			http.Error(w, "delivery diagnostic not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"diagnostic": item, "items": page.Items})
		return
	}
	deleted, err := a.history.DeleteDiagnostic(r.Context(), id)
	if err != nil {
		http.Error(w, "delivery diagnostic deletion failed", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "delivery diagnostic not found", http.StatusNotFound)
		return
	}
	a.audit(r, "delivery_diagnostic_delete", map[string]any{"diagnostic_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) SetDeliveryHistory(store *deliveryhistory.Store) { a.history = store }

func (a *API) handleDeliveryHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if next, _, ok := a.authorize(w, r, roleViewer); !ok {
		return
	} else {
		r = next
	}
	if a.history == nil {
		http.Error(w, "delivery history is unavailable", http.StatusNotImplemented)
		return
	}
	query := deliveryhistory.Query{
		MessageID: strings.TrimSpace(r.URL.Query().Get("message_id")),
		Result:    strings.TrimSpace(r.URL.Query().Get("result")),
	}
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		query.Limit, err = strconv.Atoi(value)
		if err != nil || query.Limit <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
	}
	if value := r.URL.Query().Get("after"); value != "" {
		query.AfterID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || query.AfterID < 0 {
			http.Error(w, "invalid after", http.StatusBadRequest)
			return
		}
	}
	if query.Since, err = parseHistoryTime(r.URL.Query().Get("since")); err != nil {
		http.Error(w, "invalid since", http.StatusBadRequest)
		return
	}
	if query.Until, err = parseHistoryTime(r.URL.Query().Get("until")); err != nil {
		http.Error(w, "invalid until", http.StatusBadRequest)
		return
	}
	page, err := a.history.Search(r.Context(), query)
	if err != nil {
		if strings.Contains(err.Error(), "limit must not exceed") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.ErrorContext(r.Context(), "delivery history search failed", "component", "admin", "error", err)
		http.Error(w, "delivery history search failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func parseHistoryTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func (a *API) authorize(w http.ResponseWriter, r *http.Request, min role) (*http.Request, Principal, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return r, Principal{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	principal, ok := a.authBackend.AuthenticateBearerToken(token)
	if !ok || !roleAllowed(principal.Role, min) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return r, Principal{}, false
	}
	return withPrincipal(r, principal), principal, true
}

func roleAllowed(got, min role) bool {
	order := map[role]int{roleViewer: 1, roleOperator: 2, roleAdmin: 3}
	return order[got] >= order[min]
}

func (a *API) handleSuppressions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if next, _, ok := a.authorize(w, r, roleViewer); !ok {
			return
		} else {
			r = next
		}
		if a.suppressions == nil {
			http.Error(w, "suppression admin is unavailable", http.StatusNotImplemented)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": a.suppressions.List()})
	case http.MethodPost:
		if next, _, ok := a.authorize(w, r, roleOperator); !ok {
			return
		} else {
			r = next
		}
		if a.suppressions == nil {
			http.Error(w, "suppression admin is unavailable", http.StatusNotImplemented)
			return
		}
		var req struct {
			Address string `json:"address"`
			Reason  string `json:"reason"`
			DryRun  bool   `json:"dry_run"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Address) == "" {
			http.Error(w, "address is required", http.StatusBadRequest)
			return
		}
		if !req.DryRun {
			if err := a.suppressions.Add(req.Address, req.Reason); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		a.audit(r, "suppression_add", map[string]any{"address": req.Address, "dry_run": req.DryRun})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": req.DryRun})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleSuppressionByAddress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if next, _, ok := a.authorize(w, r, roleOperator); !ok {
		return
	} else {
		r = next
	}
	if a.suppressions == nil {
		http.Error(w, "suppression admin is unavailable", http.StatusNotImplemented)
		return
	}
	address := strings.TrimPrefix(r.URL.Path, "/api/v1/suppressions/")
	if address == "" {
		http.Error(w, "address is required", http.StatusBadRequest)
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "1"
	removed := false
	var err error
	if !dryRun {
		removed, err = a.suppressions.Remove(address)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	a.audit(r, "suppression_remove", map[string]any{"address": address, "dry_run": dryRun})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "dry_run": dryRun})
}

func (a *API) handleQueue(w http.ResponseWriter, r *http.Request) {
	if a.queue == nil {
		http.Error(w, "queue admin is unavailable", http.StatusNotImplemented)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/queue/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "quarantine" && r.Method == http.MethodDelete {
		a.deleteQuarantine(w, r, parts[1])
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		if next, _, ok := a.authorize(w, r, roleViewer); !ok {
			return
		} else {
			r = next
		}
		state := strings.TrimSpace(parts[0])
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		items, err := a.queue.ListState(state, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.EqualFold(strings.TrimSpace(state), "quarantine") {
			for i, msg := range items {
				copy := *msg
				copy.Data = nil
				items[i] = &copy
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPost && parts[2] == "requeue" {
		if next, _, ok := a.authorize(w, r, roleOperator); !ok {
			return
		} else {
			r = next
		}
		state := strings.TrimSpace(parts[0])
		id := strings.TrimSpace(parts[1])
		dryRun := r.URL.Query().Get("dry_run") == "1"
		if dryRun {
			a.audit(r, "queue_requeue", map[string]any{"state": state, "message_id": id, "dry_run": true})
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true})
			return
		}
		msg, err := a.queue.RequeueFromState(state, id, a.now().UTC())
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, queue.ErrMessageNotFound) {
				code = http.StatusNotFound
			} else if errors.Is(err, queue.ErrInvalidStateTransition) {
				code = http.StatusConflict
			} else if strings.Contains(err.Error(), "unknown queue state") {
				code = http.StatusBadRequest
			}
			http.Error(w, err.Error(), code)
			return
		}
		a.audit(r, "queue_requeue", map[string]any{"state": state, "message_id": id, "dry_run": false})
		if strings.EqualFold(strings.TrimSpace(state), "quarantine") {
			copy := *msg
			copy.Data = nil
			msg = &copy
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
		return
	}
	http.NotFound(w, r)
}

func (a *API) handleComplaint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if next, _, ok := a.authorize(w, r, roleOperator); !ok {
		return
	} else {
		r = next
	}
	if a.reputation == nil {
		http.Error(w, "reputation admin is unavailable", http.StatusNotImplemented)
		return
	}
	var req struct {
		Domain string `json:"domain"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}
	if !req.DryRun {
		if err := a.reputation.RecordComplaint(req.Domain); err != nil {
			if errors.Is(err, reputation.ErrInvalidDomain) {
				http.Error(w, "invalid domain", http.StatusBadRequest)
				return
			}
			http.Error(w, "reputation storage unavailable", http.StatusInternalServerError)
			return
		}
	}
	a.audit(r, "reputation_complaint_record", map[string]any{"domain": req.Domain, "dry_run": req.DryRun})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": req.DryRun})
}

func (a *API) handleTLSReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if next, _, ok := a.authorize(w, r, roleOperator); !ok {
		return
	} else {
		r = next
	}
	if a.reputation == nil {
		http.Error(w, "reputation admin is unavailable", http.StatusNotImplemented)
		return
	}
	var req struct {
		Domain  string `json:"domain"`
		Success bool   `json:"success"`
		DryRun  bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}
	if !req.DryRun {
		if err := a.reputation.RecordTLSReport(req.Domain, req.Success); err != nil {
			if errors.Is(err, reputation.ErrInvalidDomain) {
				http.Error(w, "invalid domain", http.StatusBadRequest)
				return
			}
			http.Error(w, "reputation storage unavailable", http.StatusInternalServerError)
			return
		}
	}
	a.audit(r, "reputation_tlsrpt_record", map[string]any{"domain": req.Domain, "success": req.Success, "dry_run": req.DryRun})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": req.DryRun})
}

func (a *API) audit(r *http.Request, event string, attrs map[string]any) {
	fields := []any{
		"component", "audit",
		"event", event,
		"actor", actorFromRequest(r),
		"actor_header", actorHeaderFromRequest(r),
		"method", r.Method,
		"path", r.URL.Path,
	}
	if principal, ok := principalFromRequest(r); ok {
		fields = append(
			fields,
			"auth_principal", principal.Subject,
			"auth_role", principal.Role,
			"auth_source", principal.AuthSource,
			"token_fingerprint", principal.TokenFingerprint,
		)
	}
	for k, v := range attrs {
		fields = append(fields, k, v)
	}
	slog.Info("admin operation", fields...)
}

func actorFromRequest(r *http.Request) string {
	if v := actorHeaderFromRequest(r); v != "" {
		return v
	}
	if principal, ok := principalFromRequest(r); ok && strings.TrimSpace(principal.Subject) != "" {
		return principal.Subject
	}
	return "unknown"
}

func actorHeaderFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Admin-Actor"))
}

func withPrincipal(r *http.Request, principal Principal) *http.Request {
	ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
	return r.WithContext(ctx)
}

func principalFromRequest(r *http.Request) (Principal, bool) {
	if r == nil {
		return Principal{}, false
	}
	principal, ok := r.Context().Value(principalContextKey{}).(Principal)
	return principal, ok
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) deleteQuarantine(w http.ResponseWriter, r *http.Request, id string) {
	next, _, ok := a.authorize(w, r, roleOperator)
	if !ok {
		return
	}
	r = next
	store, ok := a.queue.(interface{ DeleteQuarantined(string, bool) error })
	if !ok {
		http.Error(w, "quarantine administration unavailable", http.StatusNotImplemented)
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "1"
	if err := store.DeleteQuarantined(id, dryRun); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, queue.ErrMessageNotFound) {
			code = http.StatusNotFound
		}
		if errors.Is(err, queue.ErrInvalidStateTransition) {
			code = http.StatusConflict
		}
		http.Error(w, "quarantine deletion failed", code)
		return
	}
	a.audit(r, "queue_quarantine_delete", map[string]any{"message_id": id, "dry_run": dryRun})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": dryRun})
}
