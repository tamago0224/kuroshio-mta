package queue

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func quarantineExpired(msg *model.Message, now time.Time) bool {
	return msg.Quarantine != nil && !msg.Quarantine.ExpiresAt.IsZero() && !msg.Quarantine.ExpiresAt.After(now)
}

// DeleteQuarantined validates under the same lock as release and admission.
// Dry runs check existence/state without modifying the file.
func (s *Store) DeleteQuarantined(id string, dryRun bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.refreshCapacityMetrics()
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return ErrMessageNotFound
	}
	path := filepath.Join(s.inbound, id+".json")
	msg, err := s.read(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrMessageNotFound
	}
	if err != nil {
		return err
	}
	if msg.Quarantine == nil {
		return ErrInvalidStateTransition
	}
	if dryRun {
		return nil
	}
	return os.Remove(path)
}

func (s *Store) expireQuarantine(path string, msg *model.Message) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.Counter("queue_quarantine_expired").Inc()
	}
	slog.Info("quarantine expired", "component", "audit", "event", "queue_quarantine_expired", "actor", "system", "message_id", msg.ID)
	return nil
}
