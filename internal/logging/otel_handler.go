package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type otelHandler struct {
	next slog.Handler
}

func newOTELHandler(next slog.Handler) slog.Handler {
	return &otelHandler{next: next}
}

func (h *otelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

func (h *otelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &otelHandler{next: h.next.WithAttrs(attrs)}
}

func (h *otelHandler) WithGroup(name string) slog.Handler {
	return &otelHandler{next: h.next.WithGroup(name)}
}
