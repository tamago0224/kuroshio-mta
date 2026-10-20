package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
)

// SetDeliveryPauses must be called before serving requests.
func (a *API) SetDeliveryPauses(store deliverypause.Store) { a.pauses = store }

func (a *API) handleDeliveryPauses(w http.ResponseWriter, r *http.Request) {
	min := roleOperator
	if r.Method == http.MethodGet {
		min = roleViewer
	}
	next, principal, ok := a.authorize(w, r, min)
	if !ok {
		return
	}
	r = next
	if a.pauses == nil {
		http.Error(w, "delivery pause administration unavailable", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		entries, err := a.pauses.List(r.Context())
		if err != nil {
			http.Error(w, "delivery pause backend unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": entries})
	case http.MethodPost:
		var req struct {
			Domain    string    `json:"domain"`
			Reason    string    `json:"reason"`
			ExpiresAt time.Time `json:"expires_at"`
			DryRun    bool      `json:"dry_run"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, "invalid pause request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid pause request", http.StatusBadRequest)
			return
		}
		entry, err := deliverypause.Validate(deliverypause.Entry{Domain: req.Domain, Reason: req.Reason, Actor: principal.Subject, ExpiresAt: req.ExpiresAt}, a.now())
		if err == nil {
			err = a.pauses.Put(r.Context(), entry, req.DryRun)
		}
		if err != nil {
			code := http.StatusServiceUnavailable
			if errors.Is(err, deliverypause.ErrInvalid) {
				code = http.StatusBadRequest
			}
			if errors.Is(err, deliverypause.ErrCapacity) {
				code = http.StatusConflict
			}
			http.Error(w, "delivery pause rejected", code)
			return
		}
		a.audit(r, "delivery_pause", map[string]any{"domain": entry.Domain, "reason": entry.Reason, "expires_at": entry.ExpiresAt, "dry_run": req.DryRun})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": req.DryRun, "pause": entry})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleDeliveryResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	next, principal, ok := a.authorize(w, r, roleOperator)
	if !ok {
		return
	}
	r = next
	if a.pauses == nil {
		http.Error(w, "delivery pause administration unavailable", http.StatusNotImplemented)
		return
	}
	domain, err := deliverypause.NormalizeDomain(strings.TrimPrefix(r.URL.Path, "/api/v1/delivery/pauses/"))
	if err != nil {
		http.Error(w, "invalid domain", http.StatusBadRequest)
		return
	}
	reason := r.URL.Query().Get("reason")
	if _, err := deliverypause.Validate(deliverypause.Entry{Domain: domain, Actor: principal.Subject, Reason: reason, ExpiresAt: a.now().Add(time.Minute)}, a.now()); err != nil {
		http.Error(w, "resume reason is required", http.StatusBadRequest)
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "1"
	removed, err := a.pauses.Remove(r.Context(), domain, dryRun)
	if err != nil {
		http.Error(w, "delivery pause backend unavailable", http.StatusServiceUnavailable)
		return
	}
	a.audit(r, "delivery_resume", map[string]any{"domain": domain, "reason": reason, "dry_run": dryRun})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed, "dry_run": dryRun})
}
