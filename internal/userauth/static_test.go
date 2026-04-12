package userauth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNewStaticAndAuthenticatePassword(t *testing.T) {
	b, err := NewStatic("alice@example.com:s3cr3t, bob@example.com:pass")
	if err != nil {
		t.Fatalf("new static: %v", err)
	}
	principal, ok := b.AuthenticatePassword("alice@example.com", "s3cr3t")
	if !ok {
		t.Fatal("alice should authenticate")
	}
	if principal.Username != "alice@example.com" {
		t.Fatalf("principal username=%q", principal.Username)
	}
	if len(principal.AllowedSenderDomains) != 0 || len(principal.AllowedSenderAddresses) != 0 {
		t.Fatalf("static principal sender scopes should be empty: %+v", principal)
	}
	if _, ok := b.AuthenticatePassword("ALICE@EXAMPLE.COM", "s3cr3t"); !ok {
		t.Fatal("username lookup should be case-insensitive")
	}
	if _, ok := b.AuthenticatePassword("bob@example.com", "wrong"); ok {
		t.Fatal("wrong password must fail")
	}
	if _, ok := b.AuthenticatePassword("charlie@example.com", "pass"); ok {
		t.Fatal("unknown user must fail")
	}
}

func TestNewStaticRejectsInvalidEntry(t *testing.T) {
	if _, err := NewStatic("alice@example.com"); err == nil {
		t.Fatal("invalid entry must fail")
	}
}

func TestNewSQLiteAndAuthenticatePassword(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "submission-auth.db")
	db := openSubmissionSQLiteForTest(t, dsn)
	seedSubmissionSQLiteForTest(t, db, "alice@example.com", "s3cr3t", true, nil)

	backend, err := NewSQLite(dsn)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	principal, ok := backend.AuthenticatePassword("ALICE@EXAMPLE.COM", "s3cr3t")
	if !ok {
		t.Fatal("alice should authenticate")
	}
	if principal.Username != "alice@example.com" {
		t.Fatalf("principal username=%q", principal.Username)
	}

	var lastAuthAt sql.NullString
	if err := db.QueryRow(`SELECT last_auth_at FROM submission_credentials LIMIT 1`).Scan(&lastAuthAt); err != nil {
		t.Fatalf("query last_auth_at: %v", err)
	}
	if !lastAuthAt.Valid || strings.TrimSpace(lastAuthAt.String) == "" {
		t.Fatalf("last_auth_at should be updated, got=%+v", lastAuthAt)
	}
}

func TestSQLiteRejectsDisabledExpiredOrWrongPassword(t *testing.T) {
	t.Run("disabled user", func(t *testing.T) {
		dsn := filepath.Join(t.TempDir(), "disabled.db")
		db := openSubmissionSQLiteForTest(t, dsn)
		seedSubmissionSQLiteForTest(t, db, "alice@example.com", "s3cr3t", false, nil)

		backend, err := NewSQLite(dsn)
		if err != nil {
			t.Fatalf("new sqlite: %v", err)
		}
		if _, ok := backend.AuthenticatePassword("alice@example.com", "s3cr3t"); ok {
			t.Fatal("disabled user must be rejected")
		}
	})

	t.Run("expired user", func(t *testing.T) {
		dsn := filepath.Join(t.TempDir(), "expired.db")
		db := openSubmissionSQLiteForTest(t, dsn)
		expiredAt := time.Now().Add(-time.Hour).UTC()
		seedSubmissionSQLiteForTest(t, db, "alice@example.com", "s3cr3t", true, &expiredAt)

		backend, err := NewSQLite(dsn)
		if err != nil {
			t.Fatalf("new sqlite: %v", err)
		}
		if _, ok := backend.AuthenticatePassword("alice@example.com", "s3cr3t"); ok {
			t.Fatal("expired user must be rejected")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		dsn := filepath.Join(t.TempDir(), "wrong-pass.db")
		db := openSubmissionSQLiteForTest(t, dsn)
		seedSubmissionSQLiteForTest(t, db, "alice@example.com", "s3cr3t", true, nil)

		backend, err := NewSQLite(dsn)
		if err != nil {
			t.Fatalf("new sqlite: %v", err)
		}
		if _, ok := backend.AuthenticatePassword("alice@example.com", "wrong"); ok {
			t.Fatal("wrong password must fail")
		}
	})
}

func openSubmissionSQLiteForTest(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE submission_credentials (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL,
	password_hash TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	expires_at TEXT,
	allowed_sender_domains TEXT,
	allowed_sender_addresses TEXT,
	description TEXT,
	last_auth_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func seedSubmissionSQLiteForTest(t *testing.T, db *sql.DB, username, password string, enabled bool, expiresAt *time.Time) {
	seedSubmissionSQLiteForTestWithScope(t, db, username, password, enabled, expiresAt, "", "")
}

func seedSubmissionSQLiteForTestWithScope(t *testing.T, db *sql.DB, username, password string, enabled bool, expiresAt *time.Time, domains, addresses string) {
	t.Helper()
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	sum := sha256.Sum256([]byte(password))
	passwordHash := hex.EncodeToString(sum[:])
	var expires any
	if expiresAt != nil {
		expires = expiresAt.UTC().Format("2006-01-02 15:04:05")
	}
	if _, err := db.Exec(`
INSERT INTO submission_credentials(username, password_hash, enabled, expires_at, allowed_sender_domains, allowed_sender_addresses, description)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, normalizeUsername(username), passwordHash, enabledInt, expires, domains, addresses, "seed"); err != nil {
		t.Fatalf("insert submission credential: %v", err)
	}
}

func TestSQLiteReturnsSenderScopes(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "scope.db")
	db := openSubmissionSQLiteForTest(t, dsn)
	seedSubmissionSQLiteForTestWithScope(t, db, "alice@example.com", "s3cr3t", true, nil, "example.com,example.org", "alerts@example.net, billing@example.net ")

	backend, err := NewSQLite(dsn)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	principal, ok := backend.AuthenticatePassword("alice@example.com", "s3cr3t")
	if !ok {
		t.Fatal("alice should authenticate")
	}
	if len(principal.AllowedSenderDomains) != 2 || principal.AllowedSenderDomains[0] != "example.com" || principal.AllowedSenderDomains[1] != "example.org" {
		t.Fatalf("allowed sender domains=%v", principal.AllowedSenderDomains)
	}
	if len(principal.AllowedSenderAddresses) != 2 || principal.AllowedSenderAddresses[0] != "alerts@example.net" || principal.AllowedSenderAddresses[1] != "billing@example.net" {
		t.Fatalf("allowed sender addresses=%v", principal.AllowedSenderAddresses)
	}
}
