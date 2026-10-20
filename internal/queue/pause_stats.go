package queue

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/util"
)

// PauseStats counts retained local messages matching active exact-domain holds.
// Each message is counted once, even with multiple paused recipients.
func (s *Store) PauseStats(domains map[string]bool, now time.Time) (int64, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	var oldest time.Duration
	for _, dir := range []string{s.inbound, s.retry, s.legacyPending} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			msg, err := s.read(filepath.Join(dir, entry.Name()))
			if err != nil {
				return 0, 0, err
			}
			if msg.Quarantine != nil {
				continue
			}
			for _, rcpt := range msg.RcptTo {
				domain, _ := util.DomainOf(rcpt)
				if domains[domain] {
					count++
					if !msg.CreatedAt.IsZero() {
						oldest = max(oldest, now.Sub(msg.CreatedAt))
					}
					break
				}
			}
		}
	}
	return count, oldest, nil
}
