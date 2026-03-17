package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/bounce"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/queue"
	"github.com/tamago0224/orinoco-mta/internal/reputation"
)

func TestSuppressionsAPIRequiresBearerToken(t *testing.T) {
	s, err := bounce.NewSuppressionStore(filepath.Join(t.TempDir(), "suppression.json"))
	if err != nil {
		t.Fatalf("new suppression store: %v", err)
	}
	api := NewAPI(s, nil, nil, "viewer-token:viewer")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/suppressions", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}
}

func TestSuppressionsAPIAddAndDelete(t *testing.T) {
	s, err := bounce.NewSuppressionStore(filepath.Join(t.TempDir(), "suppression.json"))
	if err != nil {
		t.Fatalf("new suppression store: %v", err)
	}
	api := NewAPI(s, nil, nil, "operator-token:operator")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppressions", strings.NewReader(`{"address":"user@example.com","reason":"manual"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !s.IsSuppressed("user@example.com") {
		t.Fatal("suppression was not added")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/suppressions/user@example.com", nil)
	req.Header.Set("Authorization", "Bearer operator-token")
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if s.IsSuppressed("user@example.com") {
		t.Fatal("suppression was not removed")
	}
}

func TestQueueAPIListAndRequeue(t *testing.T) {
	store, err := queue.New(filepath.Join(t.TempDir(), "queue"))
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	msg := &model.Message{ID: "m1", MailFrom: "sender@example.com", RcptTo: []string{"rcpt@example.net"}, Data: []byte("x")}
	if err := store.Enqueue(msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := store.Fail(msg, "perm"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	api := NewAPI(nil, store, nil, "viewer-token:viewer,operator-token:operator")
	api.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }

	req := httptest.NewRequest(http.MethodGet, "/api/v1/queue/dlq?limit=10", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Items []model.Message `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != "m1" {
		t.Fatalf("listed items=%+v", listed.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/queue/dlq/m1/requeue", nil)
	req.Header.Set("Authorization", "Bearer operator-token")
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("requeue status=%d body=%s", rec.Code, rec.Body.String())
	}
	inbound, err := store.ListState("inbound", 10)
	if err != nil {
		t.Fatalf("list inbound: %v", err)
	}
	if len(inbound) != 1 || inbound[0].ID != "m1" {
		t.Fatalf("inbound items=%+v", inbound)
	}
}

func TestReputationAPIRecordsComplaintAndTLSReport(t *testing.T) {
	rep := reputation.New(reputation.Config{MinSamples: 1})
	api := NewAPI(nil, nil, rep, "operator-token:operator")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reputation/complaints", strings.NewReader(`{"domain":"gmail.com"}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complaint status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/reputation/tlsrpt", strings.NewReader(`{"domain":"gmail.com","success":false}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec = httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tlsrpt status=%d body=%s", rec.Code, rec.Body.String())
	}

	snap := rep.Snapshot(time.Now().UTC())
	if len(snap) != 1 {
		t.Fatalf("snapshot len=%d", len(snap))
	}
	if snap[0].Complaints != 1 || snap[0].TLSRPTFailures != 1 {
		t.Fatalf("snapshot=%+v", snap[0])
	}
}
