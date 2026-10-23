package queue

import (
	"log/slog"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

type AcceptedEventSink interface{ RecordAccepted(*model.Message) error }

type eventBackend struct {
	next    Backend
	sink    AcceptedEventSink
	metrics *observability.Metrics
}

func WithAcceptedEvents(next Backend, sink AcceptedEventSink, metrics *observability.Metrics) Backend {
	if sink == nil {
		return next
	}
	return &eventBackend{next: next, sink: sink, metrics: metrics}
}

func (b *eventBackend) Enqueue(msg *model.Message) error {
	if err := b.next.Enqueue(msg); err != nil {
		return err
	}
	if err := b.sink.RecordAccepted(msg); err != nil {
		if b.metrics != nil {
			b.metrics.Counter("event_webhook_dropped").Inc()
		}
		slog.Error("accepted event persistence failed; message remains accepted", "component", "event_webhook", "msg_id", msg.ID, "error", err)
	}
	return nil
}
func (b *eventBackend) Due(limit int) ([]*model.Message, error)     { return b.next.Due(limit) }
func (b *eventBackend) AckSent(id string, msg *model.Message) error { return b.next.AckSent(id, msg) }
func (b *eventBackend) Retry(msg *model.Message, d time.Duration, r string) error {
	return b.next.Retry(msg, d, r)
}
func (b *eventBackend) Defer(msg *model.Message, d time.Duration) error { return b.next.Defer(msg, d) }
func (b *eventBackend) Fail(msg *model.Message, r string) error         { return b.next.Fail(msg, r) }
func (b *eventBackend) Close() error                                    { return b.next.Close() }
