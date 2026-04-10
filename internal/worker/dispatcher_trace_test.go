package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHandleMessagePropagatesTraceThroughRetry(t *testing.T) {
	exp, tp, restore := setupWorkerTraceExporter(t)
	defer restore()

	queueDir := t.TempDir()
	backend, err := queue.NewBackend(config.Config{QueueDir: queueDir})
	if err != nil {
		t.Fatalf("NewBackend: %v", err)
	}

	rootCtx, rootSpan := tp.Tracer("test").Start(context.Background(), "root")
	msg := &model.Message{
		ID:         "msg-1",
		RemoteAddr: "127.0.0.1:2525",
		Helo:       "client.example",
		MailFrom:   "alice@example.com",
		RcptTo:     []string{"bob@example.net"},
		Data:       []byte("Subject: trace\r\n\r\nhello\r\n"),
	}
	observability.InjectTraceContext(rootCtx, msg)
	originalTraceParent := msg.TraceParent
	if err := backend.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	d := &Dispatcher{
		cfg:   config.Config{},
		queue: backend,
		cl:    delivery.NewClient(config.Config{DeliveryMode: "relay"}),
	}
	d.handleMessage(context.Background(), msg)
	rootSpan.End()

	spans := waitForWorkerSpans(t, exp, "worker.handle_message")
	workerSpan := requireWorkerSpan(t, spans, "worker.handle_message")

	if workerSpan.Parent.SpanID() != rootSpan.SpanContext().SpanID() {
		t.Fatalf("worker parent=%s want root=%s", workerSpan.Parent.SpanID(), rootSpan.SpanContext().SpanID())
	}

	stored := readStoredMessage(t, filepath.Join(queueDir, "mail.retry", "msg-1.json"))
	if stored.TraceParent == "" {
		t.Fatal("stored retry message should keep trace_parent")
	}
	if stored.TraceParent == originalTraceParent {
		t.Fatal("stored retry message should update trace_parent to worker span context")
	}
	storedCtx := observability.ExtractTraceContext(context.Background(), &stored)
	storedSpanCtx := trace.SpanContextFromContext(storedCtx)
	if storedSpanCtx.SpanID() != workerSpan.SpanContext.SpanID() {
		t.Fatalf("stored span=%s want worker=%s", storedSpanCtx.SpanID(), workerSpan.SpanContext.SpanID())
	}
}

func setupWorkerTraceExporter(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider, func()) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevProvider := otel.GetTracerProvider()
	prevPropagator := otel.GetTextMapPropagator()
	prevWorkerTracer := workerTracer
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	workerTracer = tp.Tracer("github.com/tamago0224/kuroshio-mta/internal/worker")
	return exp, tp, func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevProvider)
		otel.SetTextMapPropagator(prevPropagator)
		workerTracer = prevWorkerTracer
	}
}

func waitForWorkerSpans(t *testing.T, exp *tracetest.InMemoryExporter, names ...string) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		spans := exp.GetSpans()
		if hasWorkerSpanNames(spans, names...) {
			return spans
		}
		if time.Now().After(deadline) {
			return spans
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasWorkerSpanNames(spans tracetest.SpanStubs, names ...string) bool {
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

func requireWorkerSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return tracetest.SpanStub{}
}

func readStoredMessage(t *testing.T, path string) model.Message {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	var msg model.Message
	if err := json.Unmarshal(b, &msg); err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
	return msg
}
