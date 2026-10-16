package queue

import (
	"errors"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func NewBackend(cfg config.Config) (Backend, error) {
	limits := CapacityLimits{MaxMessages: cfg.QueueMaxMessages, MaxBytes: cfg.QueueMaxBytes, LowPercent: cfg.QueueLowWatermarkPercent, MinFreeBytes: cfg.QueueMinFreeBytes}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.QueueBackend)) {
	case "", "local":
		b, err := New(cfg.QueueDir)
		if err != nil {
			return nil, err
		}
		b.limits = limits
		b.freeBytes = diskFreeBytes
		if limits.enabled() {
			b.ownerLock, err = lockQueue(b.root)
			if err != nil {
				return nil, err
			}
			if _, _, err := b.usage(); err != nil {
				b.Close()
				return nil, err
			}
			if limits.MinFreeBytes > 0 {
				if _, err := b.freeBytes(b.root); err != nil {
					b.Close()
					return nil, err
				}
			}
		}
		return wrapObservedBackend("local", b), nil
	case "kafka":
		if limits.enabled() {
			return nil, errors.New("queue capacity limits require local backend; Kafka broker backlog is not locally measurable")
		}
		b, err := NewKafka(cfg)
		if err != nil {
			return nil, err
		}
		return wrapObservedBackend("kafka", b), nil
	default:
		return nil, errors.New("unknown queue backend: " + cfg.QueueBackend)
	}
}
