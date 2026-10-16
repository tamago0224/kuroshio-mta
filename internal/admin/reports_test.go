package admin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/reputation"
)

func TestReportAPIAuthDedupAndLimits(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "reputation.json")
	tracker, err := reputation.NewPersistent(reputation.Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(nil, nil, tracker, "operator-token:operator,viewer-token:viewer")
	api.now = func() time.Time { return now }
	body := fmt.Sprintf(`{"organization-name":"provider.example","report-id":"1","date-range":{"start-datetime":%q,"end-datetime":%q},"policies":[{"policy":{"policy-domain":"example.com"},"summary":{"total-successful-session-count":5,"total-failure-session-count":2}}]}`, now.Add(-24*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	tests := []struct {
		name, token, body string
		status            int
		duplicate         bool
	}{
		{name: "unauthenticated", body: body, status: http.StatusUnauthorized},
		{name: "viewer cannot write", token: "viewer-token", body: body, status: http.StatusForbidden},
		{name: "operator", token: "operator-token", body: body, status: http.StatusOK},
		{name: "duplicate", token: "operator-token", body: body, status: http.StatusOK, duplicate: true},
		{name: "invalid report", token: "operator-token", body: "{}", status: http.StatusBadRequest},
		{name: "oversized", token: "operator-token", body: strings.Repeat("x", reputation.MaxReportBytes+1), status: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/reputation/reports/tlsrpt", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/tlsrpt+json")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, req)
			if response.Code != tt.status {
				t.Fatalf("status %d: %s", response.Code, response.Body)
			}
			if tt.status == http.StatusOK && !strings.Contains(response.Body.String(), fmt.Sprintf(`"duplicate":%t`, tt.duplicate)) {
				t.Fatal(response.Body)
			}
		})
	}
	restored, err := reputation.NewPersistent(reputation.Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	stats := restored.Snapshot(now)
	if len(stats) != 1 || stats[0].TLSRPTSuccesses != 5 || stats[0].TLSRPTFailures != 2 {
		t.Fatalf("persisted counters: %+v", stats)
	}
	metrics := tracker.RenderReportMetrics()
	for _, want := range []string{`{result="accepted"} 1`, `{result="duplicate"} 1`, `{result="rejected"} 2`} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("missing metric %s in %s", want, metrics)
		}
	}
}

func TestComplaintReportAPIRequiresIDAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reputation.json")
	tracker, err := reputation.NewPersistent(reputation.Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(nil, nil, tracker, "operator-token:operator")
	body := "Feedback-Type: abuse\r\nVersion: 1\r\nUser-Agent: collector/1\r\nOriginal-Rcpt-To: user@example.com\r\n"
	for _, id := range []string{"", "provider/report1", "provider/report1"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reputation/reports/complaints", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer operator-token")
		req.Header.Set("Content-Type", "message/feedback-report")
		req.Header.Set("Idempotency-Key", id)
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, req)
		expected := http.StatusOK
		if id == "" {
			expected = http.StatusBadRequest
		}
		if response.Code != expected {
			t.Fatalf("status %d: %s", response.Code, response.Body)
		}
	}
	restored, err := reputation.NewPersistent(reputation.Config{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot(time.Now())[0].Complaints != 1 {
		t.Fatal("complaint lost or counted twice")
	}
}

func TestReportAPIStorageErrorDoesNotAcknowledge(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	tracker, err := reputation.NewPersistent(reputation.Config{}, filepath.Join(blocker, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(nil, nil, tracker, "operator-token:operator")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reputation/reports/complaints", strings.NewReader("Feedback-Type: abuse\r\nVersion: 1\r\nUser-Agent: collector\r\nOriginal-Rcpt-To: user@example.com\r\n"))
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("Content-Type", "message/feedback-report")
	req.Header.Set("Idempotency-Key", "report1")
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", response.Code)
	}
	if len(tracker.Snapshot(time.Now())) != 0 {
		t.Fatal("failed request changed counters")
	}
	if strings.Contains(response.Body.String(), blocker) {
		t.Fatal("private filesystem path exposed")
	}
}
