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
	return s.prune(ctx, time.Now().UTC())
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
