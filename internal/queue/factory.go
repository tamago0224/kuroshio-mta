package queue

import (
	"errors"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func NewBackend(cfg config.Config) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.QueueBackend)) {
	case "", "local":
		return New(cfg.QueueDir)
	case "kafka":
		return NewKafka(cfg)
	default:
		return nil, errors.New("unknown queue backend: " + cfg.QueueBackend)
	}
}
