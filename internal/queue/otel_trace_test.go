package queue

import (
	"context"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObservedBackendRetryUsesMessageTraceContext(t *testing.T) {
	exp, tp, restore := setupQueueTraceExporter(t)
	defer restore()

	rootCtx, rootSpan := tp.Tracer("test").Start(context.Background(), "root")
	msg := &model.Message{
		ID:       "msg-1",
		RcptTo:   []string{"bob@example.net"},
		Attempts: 1,
	}
	observability.InjectTraceContext(rootCtx, msg)
	rootSpan.End()

	b := wrapObservedBackend("test", &recordingBackend{})
	if err := b.Retry(msg, 5*time.Minute, "temporary failure"); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	spans := waitForQueueSpans(t, exp, "queue.retry")
	retry := requireQueueSpan(t, spans, "queue.retry")
	if retry.Parent.SpanID() != rootSpan.SpanContext().SpanID() {
		t.Fatalf("queue.retry parent=%s want root=%s", retry.Parent.SpanID(), rootSpan.SpanContext().SpanID())
	}
	if got := queueAttrString(t, retry.Attributes, "queue.reason"); got != "temporary failure" {
		t.Fatalf("queue.reason=%q want=%q", got, "temporary failure")
	}
}

type recordingBackend struct{}

func (recordingBackend) Enqueue(*model.Message) error                      { return nil }
func (recordingBackend) Due(int) ([]*model.Message, error)                 { return nil, nil }
func (recordingBackend) AckSent(string, *model.Message) error              { return nil }
func (recordingBackend) Retry(*model.Message, time.Duration, string) error { return nil }
func (recordingBackend) Fail(*model.Message, string) error                 { return nil }
func (recordingBackend) Close() error                                      { return nil }

func setupQueueTraceExporter(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider, func()) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevProvider := otel.GetTracerProvider()
	prevPropagator := otel.GetTextMapPropagator()
	prevQueueTracer := queueTracer
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	queueTracer = tp.Tracer("github.com/tamago0224/kuroshio-mta/internal/queue")
	return exp, tp, func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevProvider)
		otel.SetTextMapPropagator(prevPropagator)
		queueTracer = prevQueueTracer
	}
}

func waitForQueueSpans(t *testing.T, exp *tracetest.InMemoryExporter, names ...string) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		spans := exp.GetSpans()
		if hasQueueSpanNames(spans, names...) {
			return spans
		}
		if time.Now().After(deadline) {
			return spans
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasQueueSpanNames(spans tracetest.SpanStubs, names ...string) bool {
	for _, name := range names {
		found := false
		for _, span := range spans {
			if span.Name == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func requireQueueSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return tracetest.SpanStub{}
}

func queueAttrString(t *testing.T, attrs []attribute.KeyValue, key string) string {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return ""
}
