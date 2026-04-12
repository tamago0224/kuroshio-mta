package admin

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
	_ "modernc.org/sqlite"
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

func TestSuppressionsAPIAcceptsSHA256Token(t *testing.T) {
	s, err := bounce.NewSuppressionStore(filepath.Join(t.TempDir(), "suppression.json"))
	if err != nil {
		t.Fatalf("new suppression store: %v", err)
	}
	token := "operator-token"
	sum := sha256.Sum256([]byte(token))
	api := NewAPI(s, nil, nil, "sha256="+hex.EncodeToString(sum[:])+":operator")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppressions", strings.NewReader(`{"address":"user@example.com","reason":"manual"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !s.IsSuppressed("user@example.com") {
		t.Fatal("suppression was not added")
	}
}

func TestConfigTokenBackendSkipsInvalidSHA256Spec(t *testing.T) {
	backend := NewConfigTokenBackend("sha256=not-hex:viewer,viewer-token:viewer")
	if _, ok := backend.AuthenticateBearerToken("viewer-token"); !ok {
		t.Fatal("expected plain token to remain available")
	}
	if _, ok := backend.AuthenticateBearerToken("not-hex"); ok {
		t.Fatal("invalid sha256 token spec must be ignored")
	}
}

func TestConfigTokenBackendReturnsFingerprintAndPrincipalMetadata(t *testing.T) {
	backend := NewConfigTokenBackend("viewer-token:viewer")
	principal, ok := backend.AuthenticateBearerToken("viewer-token")
	if !ok {
		t.Fatal("expected token authentication to succeed")
	}
	if principal.Subject != "config:viewer" {
		t.Fatalf("subject=%q want=%q", principal.Subject, "config:viewer")
	}
	if principal.AuthSource != "config_token" {
		t.Fatalf("auth source=%q want=%q", principal.AuthSource, "config_token")
	}
	sum := sha256.Sum256([]byte("viewer-token"))
	want := "sha256:" + hex.EncodeToString(sum[:8])
	if principal.TokenFingerprint != want {
		t.Fatalf("fingerprint=%q want=%q", principal.TokenFingerprint, want)
	}
}

func TestSQLiteTokenBackendAuthenticatesEnabledTokenAndUpdatesLastUsedAt(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "admin-auth.db")
	db := openAdminSQLiteForTest(t, dsn)
	seedAdminSQLiteForTest(t, db, "operator@example.com", "operator", "operator-token", true, nil)

	backend, err := NewSQLiteTokenBackend(dsn)
	if err != nil {
		t.Fatalf("new sqlite backend: %v", err)
	}
	principal, ok := backend.AuthenticateBearerToken("operator-token")
	if !ok {
		t.Fatal("expected token authentication to succeed")
	}
	if principal.Subject != "operator@example.com" {
		t.Fatalf("subject=%q want=%q", principal.Subject, "operator@example.com")
	}
	if principal.Role != roleOperator {
		t.Fatalf("role=%q want=%q", principal.Role, roleOperator)
	}
	if principal.AuthSource != "sqlite_token" {
		t.Fatalf("auth source=%q want=%q", principal.AuthSource, "sqlite_token")
	}
	sum := sha256.Sum256([]byte("operator-token"))
	want := "sha256:" + hex.EncodeToString(sum[:8])
	if principal.TokenFingerprint != want {
		t.Fatalf("fingerprint=%q want=%q", principal.TokenFingerprint, want)
	}

	var lastUsedAt sql.NullString
	if err := db.QueryRow(`SELECT last_used_at FROM admin_tokens LIMIT 1`).Scan(&lastUsedAt); err != nil {
		t.Fatalf("query last_used_at: %v", err)
	}
	if !lastUsedAt.Valid || strings.TrimSpace(lastUsedAt.String) == "" {
		t.Fatalf("last_used_at should be updated, got=%+v", lastUsedAt)
	}
}

func TestSQLiteTokenBackendRejectsDisabledOrExpiredToken(t *testing.T) {
	t.Run("disabled token", func(t *testing.T) {
		dsn := filepath.Join(t.TempDir(), "disabled.db")
		db := openAdminSQLiteForTest(t, dsn)
		seedAdminSQLiteForTest(t, db, "operator@example.com", "operator", "operator-token", false, nil)

		backend, err := NewSQLiteTokenBackend(dsn)
		if err != nil {
			t.Fatalf("new sqlite backend: %v", err)
		}
		if _, ok := backend.AuthenticateBearerToken("operator-token"); ok {
			t.Fatal("disabled token must be rejected")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		dsn := filepath.Join(t.TempDir(), "expired.db")
		db := openAdminSQLiteForTest(t, dsn)
		expiredAt := time.Now().Add(-time.Hour).UTC()
		seedAdminSQLiteForTest(t, db, "viewer@example.com", "viewer", "viewer-token", true, &expiredAt)

		backend, err := NewSQLiteTokenBackend(dsn)
		if err != nil {
			t.Fatalf("new sqlite backend: %v", err)
		}
		if _, ok := backend.AuthenticateBearerToken("viewer-token"); ok {
			t.Fatal("expired token must be rejected")
		}
	})
}

type fakeAuthBackend struct {
	principal Principal
	ok        bool
}

func (f fakeAuthBackend) AuthenticateBearerToken(string) (Principal, bool) {
	return f.principal, f.ok
}

func TestAPIUsesAuthBackend(t *testing.T) {
	s, err := bounce.NewSuppressionStore(filepath.Join(t.TempDir(), "suppression.json"))
	if err != nil {
		t.Fatalf("new suppression store: %v", err)
	}
	api := NewAPIWithBackend(s, nil, nil, fakeAuthBackend{
		principal: Principal{Role: roleOperator},
		ok:        true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppressions", strings.NewReader(`{"address":"user@example.com","reason":"manual"}`))
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !s.IsSuppressed("user@example.com") {
		t.Fatal("suppression was not added")
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

func TestAuditLogIncludesActorHeaderAndAuthPrincipal(t *testing.T) {
	s, err := bounce.NewSuppressionStore(filepath.Join(t.TempDir(), "suppression.json"))
	if err != nil {
		t.Fatalf("new suppression store: %v", err)
	}
	api := NewAPIWithBackend(s, nil, nil, fakeAuthBackend{
		principal: Principal{
			Role:             roleOperator,
			Subject:          "principal:ops-team",
			AuthSource:       "config_token",
			TokenFingerprint: "sha256:deadbeefcafebabe",
		},
		ok: true,
	})

	var buf bytes.Buffer
	prev := slog.Default()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	defer slog.SetDefault(prev)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppressions", strings.NewReader(`{"address":"user@example.com","reason":"manual","dry_run":true}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("X-Admin-Actor", "operator@example.com")
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	out := buf.String()
	for _, want := range []string{
		`"component":"audit"`,
		`"actor":"operator@example.com"`,
		`"actor_header":"operator@example.com"`,
		`"auth_principal":"principal:ops-team"`,
		`"auth_role":"operator"`,
		`"auth_source":"config_token"`,
		`"token_fingerprint":"sha256:deadbeefcafebabe"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in audit log: %q", want, out)
		}
	}
}

func openAdminSQLiteForTest(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE admin_principals (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	role TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	description TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE admin_tokens (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	principal_id INTEGER NOT NULL,
	token_hash TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	expires_at TEXT,
	last_used_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(principal_id) REFERENCES admin_principals(id)
);
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func seedAdminSQLiteForTest(t *testing.T, db *sql.DB, subject, roleName, token string, enabled bool, expiresAt *time.Time) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO admin_principals(name, role, enabled, description) VALUES (?, ?, 1, ?)`, subject, roleName, "seed")
	if err != nil {
		t.Fatalf("insert admin principal: %v", err)
	}
	principalID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("principal last insert id: %v", err)
	}

	tokenEnabled := 0
	if enabled {
		tokenEnabled = 1
	}
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])
	var expires any
	if expiresAt != nil {
		expires = expiresAt.UTC().Format("2006-01-02 15:04:05")
	}
	if _, err := db.Exec(`
INSERT INTO admin_tokens(principal_id, token_hash, enabled, expires_at)
VALUES (?, ?, ?, ?)
`, principalID, tokenHash, tokenEnabled, expires); err != nil {
		t.Fatalf("insert admin token: %v", err)
	}
}
