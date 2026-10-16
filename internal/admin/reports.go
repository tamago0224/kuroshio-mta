package admin

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/tamago0224/kuroshio-mta/internal/reputation"
)

func (a *API) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	next, _, ok := a.authorize(w, r, roleOperator)
	if !ok {
		return
	}
	r = next
	if a.reputation == nil {
		http.Error(w, "reputation admin is unavailable", http.StatusNotImplemented)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, reputation.MaxReportBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		a.reputation.RejectReport()
		var limit *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &limit) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid or oversized report", status)
		return
	}
	var report reputation.Report
	switch r.URL.Path {
	case "/api/v1/reputation/reports/complaints":
		report, err = reputation.ParseComplaint(raw, r.Header.Get("Content-Type"), r.Header.Get("Idempotency-Key"))
	case "/api/v1/reputation/reports/tlsrpt":
		report, err = reputation.ParseTLSReport(raw, r.Header.Get("Content-Type"), a.now().UTC())
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.reputation.RejectReport()
		a.audit(r, "reputation_report_rejected", map[string]any{"path": r.URL.Path})
		http.Error(w, "invalid report", http.StatusBadRequest)
		return
	}
	accepted, err := a.reputation.Ingest(report, a.now().UTC())
	if err != nil {
		slog.ErrorContext(r.Context(), "reputation report persistence failed", "component", "admin", "error", err)
		http.Error(w, "report storage unavailable", http.StatusInternalServerError)
		return
	}
	a.audit(r, "reputation_report_ingested", map[string]any{"accepted": accepted, "domains": len(report.Counts)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": accepted, "duplicate": !accepted})
}
