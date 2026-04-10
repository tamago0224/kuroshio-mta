package observability

import (
	"context"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func InjectTraceContext(ctx context.Context, msg *model.Message) {
	if msg == nil {
		return
	}
	carrier := messageCarrier{msg: msg}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

func ExtractTraceContext(ctx context.Context, msg *model.Message) context.Context {
	if msg == nil {
		return ctx
	}
	carrier := messageCarrier{msg: msg}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

type messageCarrier struct {
	msg *model.Message
}

func (c messageCarrier) Get(key string) string {
	switch key {
	case "traceparent":
		return c.msg.TraceParent
	case "tracestate":
		return c.msg.TraceState
	default:
		return ""
	}
}

func (c messageCarrier) Set(key, value string) {
	switch key {
	case "traceparent":
		c.msg.TraceParent = value
	case "tracestate":
		c.msg.TraceState = value
	}
}

func (c messageCarrier) Keys() []string {
	keys := make([]string, 0, 2)
	if c.msg.TraceParent != "" {
		keys = append(keys, "traceparent")
	}
	if c.msg.TraceState != "" {
		keys = append(keys, "tracestate")
	}
	return keys
}

var _ propagation.TextMapCarrier = messageCarrier{}
