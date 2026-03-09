package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/model"
)

type Store struct {
	root          string
	inbound       string
	retry         string
	dlq           string
	sent          string
	legacyPending string
}

func New(root string) (*Store, error) {
	s := &Store{
		root:          root,
		inbound:       filepath.Join(root, "mail.inbound"),
		retry:         filepath.Join(root, "mail.retry"),
		dlq:           filepath.Join(root, "mail.dlq"),
		sent:          filepath.Join(root, "sent"),
		legacyPending: filepath.Join(root, "pending"),
	}
	for _, d := range []string{s.root, s.inbound, s.retry, s.dlq, s.sent} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Enqueue(msg *model.Message) error {
	now := time.Now().UTC()
	msg.CreatedAt = now
	msg.UpdatedAt = now
	msg.NextAttempt = now
	return s.write(filepath.Join(s.inbound, msg.ID+".json"), msg)
}

func (s *Store) Due(limit int) ([]*model.Message, error) {
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

func (s *Store) AckSent(id string, msg *model.Message) error {
	if err := s.removeByID(id); err != nil {
		return err
	}
	msg.UpdatedAt = time.Now().UTC()
	return s.write(filepath.Join(s.sent, id+".json"), msg)
}

func (s *Store) Retry(msg *model.Message, delay time.Duration, reason string) error {
	if err := s.removeByID(msg.ID); err != nil {
		return err
	}
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	msg.NextAttempt = msg.UpdatedAt.Add(delay)
	return s.write(filepath.Join(s.retry, msg.ID+".json"), msg)
}

func (s *Store) Fail(msg *model.Message, reason string) error {
	if err := s.removeByID(msg.ID); err != nil {
		return err
	}
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	return s.write(filepath.Join(s.dlq, msg.ID+".json"), msg)
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
			continue
		}
		if msg.NextAttempt.After(now) {
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

func (s *Store) removeByID(id string) error {
	for _, dir := range []string{s.inbound, s.retry, s.legacyPending} {
		if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) write(path string, msg *model.Message) error {
	tmp := path + ".tmp"
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
