package queue

import (
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

type Backend interface {
	Enqueue(msg *model.Message) error
	Due(limit int) ([]*model.Message, error)
	AckSent(id string, msg *model.Message) error
	Retry(msg *model.Message, delay time.Duration, reason string) error
	Fail(msg *model.Message, reason string) error
	Close() error
}
