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

	"github.com/tamago/orinoco-mta/internal/model"
)

type Store struct {
	root    string
	pending string
	failed  string
	sent    string
}

func New(root string) (*Store, error) {
	s := &Store{
		root:    root,
		pending: filepath.Join(root, "pending"),
		failed:  filepath.Join(root, "failed"),
		sent:    filepath.Join(root, "sent"),
	}
	for _, d := range []string{s.root, s.pending, s.failed, s.sent} {
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
	return s.write(filepath.Join(s.pending, msg.ID+".json"), msg)
}

func (s *Store) Due(limit int) ([]*model.Message, error) {
	entries, err := os.ReadDir(s.pending)
	if err != nil {
		return nil, err
	}
	var out []*model.Message
	now := time.Now().UTC()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.pending, e.Name())
		msg, err := s.read(path)
		if err != nil {
			continue
		}
		if msg.NextAttempt.After(now) {
			continue
		}
		out = append(out, msg)
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
	if err := os.Remove(filepath.Join(s.pending, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	msg.UpdatedAt = time.Now().UTC()
	return s.write(filepath.Join(s.sent, id+".json"), msg)
}

func (s *Store) Retry(msg *model.Message, delay time.Duration, reason string) error {
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	msg.NextAttempt = msg.UpdatedAt.Add(delay)
	return s.write(filepath.Join(s.pending, msg.ID+".json"), msg)
}

func (s *Store) Fail(msg *model.Message, reason string) error {
	if err := os.Remove(filepath.Join(s.pending, msg.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	msg.Attempts++
	msg.LastError = reason
	msg.UpdatedAt = time.Now().UTC()
	return s.write(filepath.Join(s.failed, msg.ID+".json"), msg)
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
