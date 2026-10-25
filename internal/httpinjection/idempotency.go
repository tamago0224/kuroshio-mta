package httpinjection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type idempotencyRecord struct {
	Principal   string    `json:"principal"`
	Key         string    `json:"key"`
	RequestHash string    `json:"request_hash"`
	MessageID   string    `json:"message_id"`
	Committed   bool      `json:"committed"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type idempotencyStore struct {
	mu  sync.Mutex
	dir string
	ttl time.Duration
}

func openIdempotencyStore(queueDir string, ttl time.Duration) (*idempotencyStore, error) {
	dir := filepath.Join(queueDir, "http-injection-idempotency")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create HTTP injection idempotency directory: %w", err)
	}
	return &idempotencyStore{dir: dir, ttl: ttl}, nil
}

func (s *idempotencyStore) lookup(principal, key string, now time.Time) (idempotencyRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(principal, key)
	record, err := s.read(path)
	if errors.Is(err, os.ErrNotExist) {
		return idempotencyRecord{}, false, nil
	}
	if err != nil {
		return idempotencyRecord{}, false, err
	}
	if !record.ExpiresAt.After(now) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return idempotencyRecord{}, false, err
		}
		return idempotencyRecord{}, false, nil
	}
	if record.Principal != strings.ToLower(strings.TrimSpace(principal)) || record.Key != key {
		return idempotencyRecord{}, false, errors.New("idempotency record scope mismatch")
	}
	return record, true, nil
}

func (s *idempotencyStore) prepare(principal, key, requestHash, messageID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := idempotencyRecord{
		Principal: strings.ToLower(strings.TrimSpace(principal)),
		Key:       key, RequestHash: requestHash, MessageID: messageID,
		ExpiresAt: now.Add(s.ttl),
	}
	return s.write(s.path(principal, key), record)
}

func (s *idempotencyStore) commit(principal, key string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(principal, key)
	record, err := s.read(path)
	if err != nil {
		return err
	}
	record.Committed = true
	record.ExpiresAt = now.Add(s.ttl)
	return s.write(path, record)
}

func (s *idempotencyStore) path(principal, key string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(principal)) + "\x00" + key))
	return filepath.Join(s.dir, hex.EncodeToString(hash[:])+".json")
}

func (s *idempotencyStore) read(path string) (idempotencyRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return idempotencyRecord{}, err
	}
	var record idempotencyRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return idempotencyRecord{}, fmt.Errorf("decode idempotency record: %w", err)
	}
	return record, nil
}

func (s *idempotencyStore) write(path string, record idempotencyRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".idempotency-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type rateWindow struct {
	start time.Time
	count int
}

type principalLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]rateWindow
}

func newPrincipalLimiter(limit int) *principalLimiter {
	return &principalLimiter{limit: limit, windows: map[string]rateWindow{}}
}

func (l *principalLimiter) allow(principal string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(principal))
	window := l.windows[key]
	if window.start.IsZero() || now.Sub(window.start) >= time.Minute {
		l.windows[key] = rateWindow{start: now, count: 1}
		return true
	}
	if window.count >= l.limit {
		return false
	}
	window.count++
	l.windows[key] = window
	return true
}
