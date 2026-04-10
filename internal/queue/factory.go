package queue

import (
	"errors"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func NewBackend(cfg config.Config) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.QueueBackend)) {
	case "", "local":
		b, err := New(cfg.QueueDir)
		if err != nil {
			return nil, err
		}
		return wrapObservedBackend("local", b), nil
	case "kafka":
		b, err := NewKafka(cfg)
		if err != nil {
			return nil, err
		}
		return wrapObservedBackend("kafka", b), nil
	default:
		return nil, errors.New("unknown queue backend: " + cfg.QueueBackend)
	}
}
