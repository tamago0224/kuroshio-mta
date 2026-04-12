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
	suppressions *bounce.SuppressionStore
	queue        queueManager
	reputation   *reputation.Tracker
	authBackend  AuthBackend
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
	mux.HandleFunc("/api/v1/suppressions", a.handleSuppressions)
	mux.HandleFunc("/api/v1/suppressions/", a.handleSuppressionByAddress)
	mux.HandleFunc("/api/v1/queue/", a.handleQueue)
	mux.HandleFunc("/api/v1/reputation/complaints", a.handleComplaint)
	mux.HandleFunc("/api/v1/reputation/tlsrpt", a.handleTLSReport)
	return mux
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
			} else if strings.Contains(err.Error(), "unknown queue state") {
				code = http.StatusBadRequest
			}
			http.Error(w, err.Error(), code)
			return
		}
		a.audit(r, "queue_requeue", map[string]any{"state": state, "message_id": id, "dry_run": false})
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
		a.reputation.RecordComplaint(req.Domain)
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
		a.reputation.RecordTLSReport(req.Domain, req.Success)
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
