// Package deliverypause stores operator-controlled, expiring domain holds.
package deliverypause

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const MaxEntries = 1024
const MaxDuration = 30 * 24 * time.Hour

var ErrInvalid = errors.New("invalid delivery pause")
var ErrCapacity = errors.New("delivery pause capacity reached")

type Entry struct {
	Domain    string    `json:"domain"`
	Reason    string    `json:"reason"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Store operations are safe for concurrent callers. A zero Entry means no hold.
type Store interface {
	Lookup(context.Context, string) (Entry, error)
	List(context.Context) ([]Entry, error)
	Put(context.Context, Entry, bool) error
	Remove(context.Context, string, bool) (bool, error)
	Close() error
}

// NormalizeDomain accepts exact ASCII DNS domains; wildcards are unsupported.
func NormalizeDomain(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || len(domain) > 253 {
		return "", ErrInvalid
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalid
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return "", ErrInvalid
			}
		}
	}
	return domain, nil
}

func Validate(e Entry, now time.Time) (Entry, error) {
	domain, err := NormalizeDomain(e.Domain)
	if err != nil {
		return Entry{}, err
	}
	e.Domain = domain
	e.Reason = strings.TrimSpace(e.Reason)
	e.Actor = strings.TrimSpace(e.Actor)
	if e.Reason == "" || len(e.Reason) > 512 || e.Actor == "" || len(e.Actor) > 512 {
		return Entry{}, ErrInvalid
	}
	if strings.IndexFunc(e.Reason, unicode.IsControl) >= 0 || strings.IndexFunc(e.Actor, unicode.IsControl) >= 0 {
		return Entry{}, ErrInvalid
	}
	if !e.ExpiresAt.After(now) || e.ExpiresAt.After(now.Add(MaxDuration)) {
		return Entry{}, ErrInvalid
	}
	e.CreatedAt = now.UTC()
	e.ExpiresAt = e.ExpiresAt.UTC()
	return e, nil
}

type Local struct {
	mu      sync.Mutex
	path    string
	entries map[string]Entry
	now     func() time.Time
}

// NewLocal loads one process's state. The path must not be shared by processes.
func NewLocal(path string) (*Local, error) {
	s := &Local{path: path, entries: map[string]Entry{}, now: time.Now}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, errors.New("invalid delivery pause state")
	}
	if s.entries == nil || len(s.entries) > MaxEntries {
		return nil, errors.New("invalid delivery pause state")
	}
	for key, e := range s.entries {
		domain, err := NormalizeDomain(e.Domain)
		if err != nil || key != domain || e.Domain != domain || e.Reason == "" || e.Actor == "" || e.ExpiresAt.IsZero() {
			return nil, errors.New("invalid delivery pause state")
		}
	}
	return s, nil
}

func (s *Local) Lookup(ctx context.Context, domain string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[domain]
	if !e.ExpiresAt.After(s.now()) {
		return Entry{}, nil
	}
	return e, nil
}

func (s *Local) List(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entry{}
	now := s.now()
	for _, e := range s.entries {
		if e.ExpiresAt.After(now) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func (s *Local) Put(ctx context.Context, e Entry, dryRun bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	e, err := Validate(e, now)
	if err != nil {
		return err
	}
	next := s.active(now)
	next[e.Domain] = e
	if len(next) > MaxEntries {
		return ErrCapacity
	}
	if dryRun {
		return nil
	}
	return s.save(next)
}

func (s *Local) Remove(ctx context.Context, domain string, dryRun bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	domain, err := NormalizeDomain(domain)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.active(s.now())
	_, exists := next[domain]
	if dryRun || !exists {
		return exists, nil
	}
	delete(next, domain)
	return true, s.save(next)
}

func (s *Local) Close() error { return nil }

func (s *Local) active(now time.Time) map[string]Entry {
	next := map[string]Entry{}
	for domain, e := range s.entries {
		if e.ExpiresAt.After(now) {
			next[domain] = e
		}
	}
	return next
}

func (s *Local) save(next map[string]Entry) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".delivery-pauses-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.path); err != nil {
		return err
	}
	s.entries = next
	return nil
}
