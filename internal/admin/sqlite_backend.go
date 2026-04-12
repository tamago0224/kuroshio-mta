package admin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"

	_ "modernc.org/sqlite"
)

type sqliteTokenBackend struct {
	db *sql.DB
}

func NewSQLiteTokenBackend(dsn string) (AuthBackend, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("admin auth sqlite dsn is required")
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
	return &sqliteTokenBackend{db: db}, nil
}

func (b *sqliteTokenBackend) AuthenticateBearerToken(token string) (Principal, bool) {
	if b == nil || b.db == nil {
		return Principal{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, false
	}
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	var subject string
	var rawRole string
	err := b.db.QueryRow(`
SELECT p.name, p.role
FROM admin_tokens AS t
JOIN admin_principals AS p ON p.id = t.principal_id
WHERE t.token_hash = ?
  AND t.enabled = 1
  AND p.enabled = 1
  AND (t.expires_at IS NULL OR datetime(t.expires_at) > CURRENT_TIMESTAMP)
LIMIT 1
`, tokenHash).Scan(&subject, &rawRole)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("admin sqlite auth lookup failed", "component", "admin", "error", err)
		}
		return Principal{}, false
	}

	r := role(strings.ToLower(strings.TrimSpace(rawRole)))
	switch r {
	case roleViewer, roleOperator, roleAdmin:
	default:
		slog.Warn("admin sqlite auth returned unknown role", "component", "admin", "role", rawRole)
		return Principal{}, false
	}

	if _, err := b.db.Exec(`
UPDATE admin_tokens
SET last_used_at = CURRENT_TIMESTAMP
WHERE token_hash = ?
`, tokenHash); err != nil {
		slog.Error("admin sqlite auth last_used_at update failed", "component", "admin", "error", err)
	}

	return Principal{
		Role:             r,
		Subject:          strings.TrimSpace(subject),
		AuthSource:       "sqlite_token",
		TokenFingerprint: tokenFingerprint(sum),
	}, true
}
