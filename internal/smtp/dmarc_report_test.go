package smtp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Fail the first report while allowing subsequent reports to be queued.
type reportFailureQueue struct {
	recordingQueue
	attempts int
}

func (q *reportFailureQueue) Enqueue(msg *model.Message) error {
	q.attempts++
	if q.attempts == 1 {
		return errors.New("report queue unavailable")
	}
	return q.recordingQueue.Enqueue(msg)
}

func TestDMARCReportsContinueAfterEnqueueFailure(t *testing.T) {
	originalPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(originalPropagator) })
	q := &reportFailureQueue{}
	metrics := observability.NewMetrics()
	s := &Server{recipientPolicy: allowTestRecipients{}, cfg: config.Config{Hostname: "mx.example.test"}, queue: q, metrics: metrics}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), parent)
	result := mailauth.Result{DMARC: mailauth.DMARCResult{
		Result: "fail", AggregateReport: []string{"mailto:agg@example.net"},
		FailureReport: []string{"mailto:forensic@example.net"},
	}}
	s.enqueueDMARCReports(ctx, result, "alice@example.com", "original", time.Now())
	if q.attempts != 2 || len(q.msgs) != 1 {
		t.Fatalf("attempts=%d queued=%d", q.attempts, len(q.msgs))
	}
	msg := q.msgs[0]
	if msg.MailFrom != "" || len(msg.RcptTo) != 1 || msg.RcptTo[0] != "forensic@example.net" {
		t.Fatalf("unexpected report envelope: %+v", msg)
	}
	if got := trace.SpanContextFromContext(observability.ExtractTraceContext(context.Background(), msg)); got.TraceID() != parent.TraceID() {
		t.Fatalf("trace ID=%s want=%s", got.TraceID(), parent.TraceID())
	}
	if got := metrics.Counter("smtp_dmarc_report_queued").Load(); got != 1 {
		t.Fatalf("queued metric=%d want=1", got)
	}
	s.submission = true
	s.enqueueDMARCReports(ctx, result, "alice@example.com", "original", time.Now())
	if q.attempts != 2 {
		t.Fatal("submission generated reports")
	}
}

func TestBuildReportMessageNormalization(t *testing.T) {
	got := string(buildReportMessage(
		"postmaster@example.com\r\n", "report@example.net\n", "report\r\nsubject",
		[]byte("first\nsecond\rthird\r\nfourth"), "id\r\nvalue", time.Unix(0, 0),
	))
	if strings.ContainsAny(strings.ReplaceAll(got, "\r\n", ""), "\r\n") {
		t.Fatalf("non-CRLF line ending: %q", got)
	}
	for _, want := range []string{
		"From: postmaster@example.com\r\n", "To: report@example.net\r\n",
		"Subject: reportsubject\r\n", "Message-ID: <idvalue>\r\n",
		"\r\n\r\nfirst\r\nsecond\r\nthird\r\nfourth\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestQueueMessageEnqueuesDMARCReports(t *testing.T) {
	origEval := evaluateAuthWithPolicy
	evaluateAuthWithPolicy = func(_ net.IP, _, _ string, _ []byte, _ mailauth.SPFPolicy) mailauth.Result {
		return mailauth.Result{
			Action: mailauth.ActionAccept,
			DMARC: mailauth.DMARCResult{
				Domain:          "example.com",
				Result:          "fail",
				Policy:          "reject",
				Reason:          "alignment failed",
				AggregateReport: []string{"mailto:agg@example.net"},
				FailureReport:   []string{"mailto:forensic@example.net"},
			},
		}
	}
	defer func() {
		evaluateAuthWithPolicy = origEval
	}()

	q := &recordingQueue{}
	s := &Server{recipientPolicy: allowTestRecipients{}, cfg: config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024 * 1024}, queue: q}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r) // banner

	mustWriteSMTPLine(t, w, "EHLO client.example")
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "MAIL FROM:<alice@example.com>")
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "RCPT TO:<bob@example.com>")
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "DATA")
	_, dataCode := readSMTPResponse(t, r)
	if dataCode != 354 {
		t.Fatalf("data code=%d want=354", dataCode)
	}

	data := "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: test\r\n\r\nhello\r\n.\r\n"
	if _, err := w.WriteString(data); err != nil {
		t.Fatalf("write data: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush data: %v", err)
	}
	_, code := readSMTPResponse(t, r)
	if code != 250 {
		t.Fatalf("code=%d want=250", code)
	}
	if len(q.msgs) != 3 {
		t.Fatalf("queued=%d want=3", len(q.msgs))
	}
	if q.msgs[1].MailFrom != "" || len(q.msgs[1].RcptTo) != 1 || q.msgs[1].RcptTo[0] != "agg@example.net" {
		t.Fatalf("aggregate report envelope mismatch: %+v", q.msgs[1])
	}
	if q.msgs[2].MailFrom != "" || len(q.msgs[2].RcptTo) != 1 || q.msgs[2].RcptTo[0] != "forensic@example.net" {
		t.Fatalf("failure report envelope mismatch: %+v", q.msgs[2])
	}
	if !strings.Contains(string(q.msgs[1].Data), "Subject: DMARC aggregate report for example.com") {
		t.Fatalf("missing aggregate report subject: %q", string(q.msgs[1].Data))
	}
	if !strings.Contains(string(q.msgs[2].Data), "Subject: DMARC failure report for example.com") {
		t.Fatalf("missing failure report subject: %q", string(q.msgs[2].Data))
	}
}
