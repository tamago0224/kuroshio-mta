package queue

import (
	"context"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var queueTracer = otel.Tracer("github.com/tamago0224/kuroshio-mta/internal/queue")

type observedBackend struct {
	name string
	next Backend
}

func wrapObservedBackend(name string, next Backend) Backend {
	if next == nil {
		return nil
	}
	return &observedBackend{name: name, next: next}
}

func (b *observedBackend) Enqueue(msg *model.Message) error {
	_, span := b.startSpan(context.Background(), "queue.enqueue", msg)
	defer span.End()

	err := b.next.Enqueue(msg)
	recordSpanError(span, err)
	return err
}

func (b *observedBackend) Due(limit int) ([]*model.Message, error) {
	_, span := queueTracer.Start(
		context.Background(),
		"queue.due",
		trace.WithAttributes(
			attribute.String("queue.backend", b.name),
			attribute.Int("queue.limit", limit),
		),
	)
	defer span.End()

	msgs, err := b.next.Due(limit)
	if err == nil {
		span.SetAttributes(attribute.Int("queue.message_count", len(msgs)))
	}
	recordSpanError(span, err)
	return msgs, err
}

func (b *observedBackend) AckSent(id string, msg *model.Message) error {
	_, span := b.startSpan(context.Background(), "queue.ack_sent", msg)
	defer span.End()

	err := b.next.AckSent(id, msg)
	recordSpanError(span, err)
	return err
}

func (b *observedBackend) Retry(msg *model.Message, delay time.Duration, reason string) error {
	_, span := b.startSpan(context.Background(), "queue.retry", msg)
	span.SetAttributes(attribute.String("queue.retry_delay", delay.String()))
	defer span.End()

	err := b.next.Retry(msg, delay, reason)
	recordSpanError(span, err)
	return err
}

func (b *observedBackend) Fail(msg *model.Message, reason string) error {
	_, span := b.startSpan(context.Background(), "queue.fail", msg)
	defer span.End()

	err := b.next.Fail(msg, reason)
	recordSpanError(span, err)
	return err
}

func (b *observedBackend) Close() error {
	_, span := queueTracer.Start(
		context.Background(),
		"queue.close",
		trace.WithAttributes(attribute.String("queue.backend", b.name)),
	)
	defer span.End()

	err := b.next.Close()
	recordSpanError(span, err)
	return err
}

func (b *observedBackend) startSpan(ctx context.Context, name string, msg *model.Message) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attribute.String("queue.backend", b.name),
	}
	if msg != nil {
		attrs = append(attrs,
			attribute.String("mail.message_id", msg.ID),
			attribute.Int("mail.attempt", msg.Attempts),
			attribute.Int("mail.rcpt_count", len(msg.RcptTo)),
		)
	}
	return queueTracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func recordSpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
