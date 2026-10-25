package deliveryhistory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const maxPageSize = 200

type Event struct {
	ID          int64     `json:"id"`
	MessageID   string    `json:"message_id"`
	Recipient   string    `json:"recipient,omitempty"`
	Attempt     int       `json:"attempt"`
	Result      string    `json:"result"`
	Category    string    `json:"category,omitempty"`
	Stage       string    `json:"stage,omitempty"`
	SMTPCode    int       `json:"smtp_code,omitempty"`
	Route       string    `json:"route,omitempty"`
	Mode        string    `json:"mode,omitempty"`
	TLSRequired bool      `json:"tls_required"`
	Detail      string    `json:"detail,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type Query struct {
	MessageID string
	Result    string
	Since     time.Time
	Until     time.Time
	Limit     int
	AfterID   int64
}

type Page struct {
	Items     []Event `json:"items"`
	NextAfter int64   `json:"next_after,omitempty"`
}

type Diagnostic struct {
	ID        int64     `json:"id"`
	MessageID string    `json:"message_id"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxEvents int       `json:"max_events"`
	Events    int       `json:"events"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db        *sql.DB
	retention time.Duration
	maxRows   int
}

func Open(path string, retention time.Duration, maxRows int) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("delivery history path is required")
	}
	if retention <= 0 || retention > 365*24*time.Hour {
		return nil, errors.New("delivery history retention must be between 1ns and 365d")
	}
	if maxRows < 1 || maxRows > 10_000_000 {
		return nil, errors.New("delivery history max_rows must be between 1 and 10000000")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open delivery history: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, retention: retention, maxRows: maxRows}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS delivery_history (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 message_id TEXT NOT NULL,
 recipient TEXT NOT NULL DEFAULT '',
 attempt INTEGER NOT NULL,
 result TEXT NOT NULL,
 category TEXT NOT NULL DEFAULT '',
 stage TEXT NOT NULL DEFAULT '',
 smtp_code INTEGER NOT NULL DEFAULT 0,
 route TEXT NOT NULL DEFAULT '',
 mode TEXT NOT NULL DEFAULT '',
 tls_required INTEGER NOT NULL DEFAULT 0,
 detail TEXT NOT NULL DEFAULT '',
 occurred_at TEXT NOT NULL,
 UNIQUE(message_id, recipient, attempt, result, occurred_at)
);
CREATE INDEX IF NOT EXISTS delivery_history_message_id ON delivery_history(message_id, id);
CREATE INDEX IF NOT EXISTS delivery_history_occurred_at ON delivery_history(occurred_at, id);
CREATE INDEX IF NOT EXISTS delivery_history_result ON delivery_history(result, id);`)
	if err == nil {
		_, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS delivery_diagnostics (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 message_id TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 max_events INTEGER NOT NULL,
 events INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL
); CREATE INDEX IF NOT EXISTS delivery_diagnostics_target ON delivery_diagnostics(message_id, expires_at);`)
	}
	if err != nil {
		return fmt.Errorf("initialize delivery history: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Record(ctx context.Context, event Event) error {
	event.MessageID = strings.TrimSpace(event.MessageID)
	event.Result = strings.TrimSpace(event.Result)
	if event.MessageID == "" || event.Result == "" {
		return errors.New("delivery history message_id and result are required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO delivery_history
(message_id,recipient,attempt,result,category,stage,smtp_code,route,mode,tls_required,detail,occurred_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, event.MessageID, event.Recipient, event.Attempt, event.Result,
		event.Category, event.Stage, event.SMTPCode, event.Route, event.Mode, event.TLSRequired,
		truncate(event.Detail, 512), event.OccurredAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record delivery history: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE delivery_diagnostics SET events = events + 1
WHERE message_id = ? AND expires_at > ? AND events < max_events`, event.MessageID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("update delivery diagnostic: %w", err)
	}
	return s.prune(ctx, time.Now().UTC())
}

func (s *Store) CreateDiagnostic(ctx context.Context, messageID string, expiresAt time.Time, maxEvents int) (Diagnostic, error) {
	messageID = strings.TrimSpace(messageID)
	now := time.Now().UTC()
	if messageID == "" || !expiresAt.After(now) || expiresAt.After(now.Add(24*time.Hour)) {
		return Diagnostic{}, errors.New("diagnostic requires message_id and expiry within 24h")
	}
	if maxEvents < 1 || maxEvents > 1000 {
		return Diagnostic{}, errors.New("diagnostic max_events must be between 1 and 1000")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO delivery_diagnostics(message_id,expires_at,max_events,created_at) VALUES(?,?,?,?)`, messageID, expiresAt.UTC().Format(time.RFC3339Nano), maxEvents, now.Format(time.RFC3339Nano))
	if err != nil {
		return Diagnostic{}, fmt.Errorf("create delivery diagnostic: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Diagnostic{}, fmt.Errorf("read delivery diagnostic id: %w", err)
	}
	return Diagnostic{ID: id, MessageID: messageID, ExpiresAt: expiresAt.UTC(), MaxEvents: maxEvents, CreatedAt: now}, nil
}

func (s *Store) ListDiagnostics(ctx context.Context) ([]Diagnostic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,message_id,expires_at,max_events,events,created_at FROM delivery_diagnostics WHERE expires_at > ? ORDER BY id`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("list delivery diagnostics: %w", err)
	}
	defer rows.Close()
	items := []Diagnostic{}
	for rows.Next() {
		var item Diagnostic
		var expiresAt, createdAt string
		if err := rows.Scan(&item.ID, &item.MessageID, &expiresAt, &item.MaxEvents, &item.Events, &createdAt); err != nil {
			return nil, err
		}
		item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteDiagnostic(ctx context.Context, id int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM delivery_diagnostics WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete delivery diagnostic: %w", err)
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *Store) DiagnosticHistory(ctx context.Context, id int64) (Diagnostic, Page, error) {
	var item Diagnostic
	var expiresAt, createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id,message_id,expires_at,max_events,events,created_at FROM delivery_diagnostics WHERE id = ?`, id).Scan(&item.ID, &item.MessageID, &expiresAt, &item.MaxEvents, &item.Events, &createdAt)
	if err != nil {
		return Diagnostic{}, Page{}, fmt.Errorf("find delivery diagnostic: %w", err)
	}
	item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return Diagnostic{}, Page{}, err
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Diagnostic{}, Page{}, err
	}
	page, err := s.Search(ctx, Query{MessageID: item.MessageID, Since: item.CreatedAt, Until: item.ExpiresAt, Limit: min(item.MaxEvents, maxPageSize)})
	return item, page, err
}

func (s *Store) prune(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM delivery_history WHERE occurred_at < ?`, now.Add(-s.retention).UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("prune expired delivery history: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM delivery_history WHERE id IN (
 SELECT id FROM delivery_history ORDER BY id DESC LIMIT -1 OFFSET ?
)`, s.maxRows)
	if err != nil {
		return fmt.Errorf("prune excess delivery history: %w", err)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, query Query) (Page, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > maxPageSize {
		return Page{}, fmt.Errorf("limit must not exceed %d", maxPageSize)
	}
	clauses := []string{"id > ?"}
	args := []any{query.AfterID}
	if query.MessageID != "" {
		clauses = append(clauses, "message_id = ?")
		args = append(args, query.MessageID)
	}
	if query.Result != "" {
		clauses = append(clauses, "result = ?")
		args = append(args, query.Result)
	}
	if !query.Since.IsZero() {
		clauses = append(clauses, "occurred_at >= ?")
		args = append(args, query.Since.UTC().Format(time.RFC3339Nano))
	}
	if !query.Until.IsZero() {
		clauses = append(clauses, "occurred_at < ?")
		args = append(args, query.Until.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT id,message_id,recipient,attempt,result,category,stage,smtp_code,route,mode,tls_required,detail,occurred_at
FROM delivery_history WHERE `+strings.Join(clauses, " AND ")+` ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return Page{}, fmt.Errorf("search delivery history: %w", err)
	}
	defer rows.Close()
	items := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var occurredAt string
		if err := rows.Scan(&event.ID, &event.MessageID, &event.Recipient, &event.Attempt, &event.Result,
			&event.Category, &event.Stage, &event.SMTPCode, &event.Route, &event.Mode, &event.TLSRequired,
			&event.Detail, &occurredAt); err != nil {
			return Page{}, fmt.Errorf("scan delivery history: %w", err)
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return Page{}, fmt.Errorf("parse delivery history timestamp: %w", err)
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate delivery history: %w", err)
	}
	page := Page{Items: items}
	if len(items) > limit {
		page.NextAfter = items[limit-1].ID
		page.Items = items[:limit]
	}
	return page, nil
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
