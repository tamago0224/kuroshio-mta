package smtp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dmarcreport"
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

func TestRecordDMARCObservationStoresAuthorizedDomainOnlyData(t *testing.T) {
	oldOrg, oldLookup := dmarcReportOrganizationalDomain, dmarcReportLookupTXT
	dmarcReportOrganizationalDomain = func(context.Context, string) (string, error) { return "vendor.test", nil }
	dmarcReportLookupTXT = func(_ context.Context, name string) ([]string, error) {
		if name != "example.test._report._dmarc.reports.vendor.test" {
			t.Fatalf("authorization query=%q", name)
		}
		return []string{"v=DMARC1; x-vendor=opaque"}, nil
	}
	t.Cleanup(func() { dmarcReportOrganizationalDomain, dmarcReportLookupTXT = oldOrg, oldLookup })
	store, err := dmarcreport.Open(context.Background(), filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{dmarcReports: store, metrics: observability.NewMetrics()}
	now := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	auth := mailauth.Result{SPF: mailauth.SPFResult{Domain: "bounce.example.test", Result: "pass"}, DMARC: mailauth.DMARCResult{DMARCbis: &mailauth.DMARCbisResult{
		Result: "fail", AuthorDomain: "mail.example.test", PolicyDomain: "example.test", OrgDomain: "example.test", Policy: "reject", PublishedP: "reject", Discovery: "treewalk", AggregateReport: []string{"mailto:agg@reports.vendor.test"},
	}}}
	s.recordDMARCObservation(context.Background(), auth, "sender@example.test", []string{"user@recipient.test"}, net.ParseIP("192.0.2.8"), []byte("From: sender@example.test\r\n\r\nbody\r\n"), now)
	rows, err := store.Snapshot(context.Background(), now)
	if err != nil || len(rows) != 1 {
		t.Fatalf("snapshot rows=%d err=%v", len(rows), err)
	}
	ob := rows[0].Observation
	if rows[0].Count != 1 || ob.Disposition != "reject" || ob.EnvelopeFrom != "example.test" || ob.EnvelopeTo != "recipient.test" || len(ob.Recipients) != 1 {
		t.Fatalf("unexpected stored aggregate: %+v", rows[0])
	}
	s.recordDMARCObservation(context.Background(), auth, "sender@example.test", []string{"user@recipient.test"}, net.ParseIP("192.0.2.8"), []byte("From: sender@example.test\r\nX-Kuroshio-DMARC-Report: aggregate\r\n\r\nreport\r\n"), now)
	rows, err = store.Snapshot(context.Background(), now)
	if err != nil || len(rows) != 1 || rows[0].Count != 1 {
		t.Fatalf("report loop was counted: rows=%+v err=%v", rows, err)
	}
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
	if q.attempts != 1 || len(q.msgs) != 0 {
		t.Fatalf("attempts=%d queued=%d", q.attempts, len(q.msgs))
	}
	if got := metrics.Counter("smtp_dmarc_report_queued").Load(); got != 0 {
		t.Fatalf("queued metric=%d want=0", got)
	}
	s.submission = true
	s.enqueueDMARCReports(ctx, result, "alice@example.com", "original", time.Now())
	if q.attempts != 1 {
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
	if len(q.msgs) != 2 {
		t.Fatalf("queued=%d want=2", len(q.msgs))
	}
	if q.msgs[1].MailFrom != "" || len(q.msgs[1].RcptTo) != 1 || q.msgs[1].RcptTo[0] != "forensic@example.net" {
		t.Fatalf("failure report envelope mismatch: %+v", q.msgs[1])
	}
	if !strings.Contains(string(q.msgs[1].Data), "Subject: DMARC failure report for example.com") {
		t.Fatalf("missing failure report subject: %q", string(q.msgs[1].Data))
	}
}
