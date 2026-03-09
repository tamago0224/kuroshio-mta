package bounce

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SuppressionEntry struct {
	Address   string    `json:"address"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type SuppressionStore struct {
	path    string
	mu      sync.RWMutex
	entries map[string]SuppressionEntry
}

func NewSuppressionStore(path string) (*SuppressionStore, error) {
	s := &SuppressionStore{path: path, entries: map[string]SuppressionEntry{}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SuppressionStore) IsSuppressed(addr string) bool {
	n := normalizeAddress(addr)
	if n == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.entries[n]
	return ok
}

func (s *SuppressionStore) Add(addr, reason string) error {
	n := normalizeAddress(addr)
	if n == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[n]; ok {
		return nil
	}
	s.entries[n] = SuppressionEntry{Address: n, Reason: reason, CreatedAt: time.Now().UTC()}
	return s.saveLocked()
}

func (s *SuppressionStore) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	var list []SuppressionEntry
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	for _, e := range list {
		s.entries[normalizeAddress(e.Address)] = e
	}
	return nil
}

func (s *SuppressionStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	list := make([]SuppressionEntry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(strings.ToLower(addr))
	if addr == "" || !strings.Contains(addr, "@") {
		return ""
	}
	return addr
}
