package userauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLiteBackend struct {
	db *sql.DB
}

func NewSQLite(dsn string) (*SQLiteBackend, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("submission auth sqlite dsn is required")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteBackend{db: db}, nil
}

func (b *SQLiteBackend) AuthenticatePassword(username, password string) (Principal, bool) {
	if b == nil || b.db == nil {
		return Principal{}, false
	}
	user := normalizeUsername(username)
	password = strings.TrimSpace(password)
	if user == "" || password == "" {
		return Principal{}, false
	}

	var storedHash string
	var allowedDomains sql.NullString
	var allowedAddresses sql.NullString
	err := b.db.QueryRow(`
SELECT password_hash, allowed_sender_domains, allowed_sender_addresses
FROM submission_credentials
WHERE username = ?
  AND enabled = 1
  AND (expires_at IS NULL OR datetime(expires_at) > CURRENT_TIMESTAMP)
LIMIT 1
`, user).Scan(&storedHash, &allowedDomains, &allowedAddresses)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("submission sqlite auth lookup failed", "component", "smtp", "error", err, "username", user)
		}
		return Principal{}, false
	}

	sum := sha256.Sum256([]byte(password))
	gotHash := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(storedHash))), []byte(gotHash)) != 1 {
		return Principal{}, false
	}

	if _, err := b.db.Exec(`
UPDATE submission_credentials
SET last_auth_at = CURRENT_TIMESTAMP
WHERE username = ?
`, user); err != nil {
		slog.Error("submission sqlite auth last_auth_at update failed", "component", "smtp", "error", err, "username", user)
	}

	return Principal{
		Username:               user,
		AllowedSenderDomains:   parseCSVAllowList(allowedDomains.String),
		AllowedSenderAddresses: parseCSVAllowList(allowedAddresses.String),
	}, true
}

func parseCSVAllowList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	items := make([]string, 0, 4)
	for _, part := range strings.Split(v, ",") {
		part = normalizeUsername(part)
		if part == "" {
			continue
		}
		items = append(items, part)
	}
	if len(items) == 0 {
		return nil
	}
	return items
}
