package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

type Store struct {
	ownerLock *os.File
	mu        sync.Mutex
	limits    CapacityLimits
	paused    bool
	metrics   *observability.Metrics
	freeBytes func(string) (uint64, error)

	root          string
	inbound       string
	retry         string
	dlq           string
	poison        string
	sent          string
	legacyPending string
}

var (
	ErrMessageNotFound        = errors.New("message not found")
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

func New(root string) (*Store, error) {
	s := &Store{
		root:          root,
		inbound:       filepath.Join(root, "mail.inbound"),
		retry:         filepath.Join(root, "mail.retry"),
		dlq:           filepath.Join(root, "mail.dlq"),
		poison:        filepath.Join(root, "mail.dlq", "poison"),
		sent:          filepath.Join(root, "sent"),
		legacyPending: filepath.Join(root, "pending"),
	}
	for _, d := range []string{s.root, s.inbound, s.retry, s.dlq, s.poison, s.sent} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Enqueue(msg *model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	if strings.TrimSpace(msg.ID) == "" {
		return errors.New("message id is required")
	}
	st, err := s.messageState(msg.ID)
	if err != nil {
		return err
	}
	if st != "none" {
		// Idempotent enqueue: duplicate message id is ignored.
		return nil
	}
	now := time.Now().UTC()
	msg.CreatedAt = now
	msg.UpdatedAt = now
	msg.NextAttempt = now
	if err := s.admit(msg); err != nil {
		return err
	}
	return s.write(filepath.Join(s.inbound, msg.ID+".json"), msg)
}

func (s *Store) Due(limit int) ([]*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	var out []*model.Message
	for _, dir := range []string{s.inbound, s.retry, s.legacyPending} {
		msgs, err := s.dueFromDir(dir)
		if err != nil {
			return nil, err
		}
		out = append(out, msgs...)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NextAttempt.Before(out[j].NextAttempt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) ListState(state string, limit int) ([]*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	dir, err := s.dirForState(state)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*model.Message, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		msg, err := s.read(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if (state == "quarantine") != (msg.Quarantine != nil) {
			continue
		}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) RequeueFromState(state, id string, now time.Time) (*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return nil, ErrMessageNotFound
	}
	state = strings.ToLower(strings.TrimSpace(state))
	dir, err := s.dirForState(state)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	msg, err := s.read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}
	if (state == "quarantine") != (msg.Quarantine != nil) {
		return nil, ErrInvalidStateTransition
	}
	msg.Quarantine = nil
	msg.UpdatedAt = now.UTC()
	msg.NextAttempt = now.UTC()
	if dir == s.sent || dir == s.dlq {
		if err := s.admit(msg); err != nil {
			return nil, err
		}
	}
	target := filepath.Join(s.inbound, id+".json")
	if err := s.write(target, msg); err != nil {
		return nil, err
	}
	if path != target {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	return msg, nil
}

func (s *Store) AckSent(id string, msg *model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	st, err := s.messageState(id)
	if err != nil {
		return err
	}
	switch st {
	case "sent":
		return nil
	case "dlq", "quarantine":
		return ErrInvalidStateTransition
	case "none":
		return ErrMessageNotFound
	}
	msg.UpdatedAt = time.Now().UTC()
	if err := s.write(filepath.Join(s.sent, id+".json"), msg); err != nil {
		return err
	}
	return s.removePendingExcept(id, s.sent)
}

func (s *Store) Retry(msg *model.Message, delay time.Duration, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	st, err := s.messageState(msg.ID)
	if err != nil {
		return err
	}
	switch st {
	case "sent", "dlq", "quarantine":
		return ErrInvalidStateTransition
	case "none":
		return ErrMessageNotFound
	}
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	msg.NextAttempt = msg.UpdatedAt.Add(delay)
	if err := s.write(filepath.Join(s.retry, msg.ID+".json"), msg); err != nil {
		return err
	}
	return s.removePendingExcept(msg.ID, s.retry)
}

func (s *Store) Defer(msg *model.Message, delay time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	st, err := s.messageState(msg.ID)
	if err != nil {
		return err
	}
	switch st {
	case "sent", "dlq", "quarantine":
		return ErrInvalidStateTransition
	case "none":
		return ErrMessageNotFound
	}
	msg.UpdatedAt = time.Now().UTC()
	msg.NextAttempt = msg.UpdatedAt.Add(delay)
	if err := s.write(filepath.Join(s.retry, msg.ID+".json"), msg); err != nil {
		return err
	}
	return s.removePendingExcept(msg.ID, s.retry)
}

func (s *Store) Fail(msg *model.Message, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	st, err := s.messageState(msg.ID)
	if err != nil {
		return err
	}
	switch st {
	case "dlq":
		return nil
	case "sent", "quarantine":
		return ErrInvalidStateTransition
	case "none":
		return ErrMessageNotFound
	}
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	if err := s.write(filepath.Join(s.dlq, msg.ID+".json"), msg); err != nil {
		return err
	}
	return s.removePendingExcept(msg.ID, s.dlq)
}

func (s *Store) dueFromDir(dir string) ([]*model.Message, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*model.Message, 0, len(entries))
	now := time.Now().UTC()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		msg, err := s.read(path)
		if err != nil {
			_ = s.quarantinePoison(path, e.Name(), err)
			continue
		}
		if msg.Quarantine != nil || msg.NextAttempt.After(now) {
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

func (s *Store) removePendingExcept(id, except string) error {
	for _, dir := range []string{s.inbound, s.retry, s.legacyPending} {
		if dir == except {
			continue
		}
		if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerLock != nil {
		err := s.ownerLock.Close()
		s.ownerLock = nil
		return err
	}
	return nil
}

func (s *Store) write(path string, msg *model.Message) error {
	tmp := path + ".tmp"
	defer os.Remove(tmp)
	b, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) read(path string) (*model.Message, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var msg model.Message
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &msg, nil
}

func (s *Store) quarantinePoison(path, name string, cause error) error {
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		return readErr
	}
	dst := filepath.Join(s.poison, fmt.Sprintf("%d_%s.bad", time.Now().UTC().UnixNano(), strings.TrimSuffix(name, ".json")))
	payload := append([]byte(fmt.Sprintf("cause: %v\n", cause)), b...)
	if err := os.WriteFile(dst, payload, 0o644); err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *Store) messageState(id string) (string, error) {
	exists := func(path string) (bool, error) {
		_, err := os.Stat(path)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, p := range []struct {
		state string
		dir   string
	}{
		{state: "inbound", dir: s.inbound},
		{state: "retry", dir: s.retry},
		{state: "pending", dir: s.legacyPending},
		{state: "sent", dir: s.sent},
		{state: "dlq", dir: s.dlq},
	} {
		ok, err := exists(filepath.Join(p.dir, id+".json"))
		if err != nil {
			return "", err
		}
		if ok {
			if p.state == "inbound" {
				msg, err := s.read(filepath.Join(p.dir, id+".json"))
				if err != nil {
					return "", err
				}
				if msg.Quarantine != nil {
					return "quarantine", nil
				}
			}
			return p.state, nil
		}
	}
	return "none", nil
}

func (s *Store) dirForState(state string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "inbound", "quarantine":
		return s.inbound, nil
	case "retry":
		return s.retry, nil
	case "dlq":
		return s.dlq, nil
	case "sent":
		return s.sent, nil
	default:
		return "", errors.New("unknown queue state: " + state)
	}
}
