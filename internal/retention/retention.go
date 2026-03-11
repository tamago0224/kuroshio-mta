package retention

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type Policy struct {
	SentTTL   time.Duration
	DLQTTL    time.Duration
	PoisonTTL time.Duration
	Interval  time.Duration
}

func Run(ctx context.Context, queueRoot string, p Policy) error {
	if p.Interval <= 0 {
		p.Interval = time.Hour
	}
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	_ = sweep(queueRoot, p)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := sweep(queueRoot, p); err != nil {
				slog.Error("retention sweep failed", "component", "retention", "error", err)
			}
		}
	}
}

func sweep(queueRoot string, p Policy) error {
	if queueRoot == "" {
		return nil
	}
	now := time.Now().UTC()
	deleted := 0
	if n, err := removeOlder(filepath.Join(queueRoot, "sent"), p.SentTTL, now); err == nil {
		deleted += n
	} else {
		return err
	}
	if n, err := removeOlder(filepath.Join(queueRoot, "mail.dlq"), p.DLQTTL, now); err == nil {
		deleted += n
	} else {
		return err
	}
	if n, err := removeOlder(filepath.Join(queueRoot, "mail.dlq", "poison"), p.PoisonTTL, now); err == nil {
		deleted += n
	} else {
		return err
	}
	slog.Info("retention sweep completed", "component", "retention", "deleted_files", deleted)
	return nil
}

func removeOlder(dir string, ttl time.Duration, now time.Time) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := now.Add(-ttl)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		if st.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
	}
	return removed, nil
}
