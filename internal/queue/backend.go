package queue

import (
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

type Backend interface {
	// Enqueue accepts only after persistence succeeds. ErrCapacity is retryable.
	// Local capacity checks reserve admission through the atomic write; a failed
	// write releases the reservation. Kafka does not support capacity limits.
	Enqueue(msg *model.Message) error
	Due(limit int) ([]*model.Message, error)
	AckSent(id string, msg *model.Message) error
	Retry(msg *model.Message, delay time.Duration, reason string) error
	// Defer updates NextAttempt without incrementing Attempts or emitting a DSN.
	Defer(msg *model.Message, delay time.Duration) error
	Fail(msg *model.Message, reason string) error
	Close() error
}
