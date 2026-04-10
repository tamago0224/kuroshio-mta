package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSMTPTracingCapturesMailRcptAndDataSpans(t *testing.T) {
	origEval := evaluateAuthWithPolicy
	evaluateAuthWithPolicy = func(_ net.IP, _, _ string, _ []byte, _ mailauth.SPFPolicy) mailauth.Result {
		return mailauth.Result{Action: mailauth.ActionAccept}
	}
	defer func() {
		evaluateAuthWithPolicy = origEval
	}()

	exp := setupSMTPTraceExporter(t)
	q := &recordingQueue{}
	s := &Server{cfg: config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024 * 1024}, queue: q}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)

	mustWriteSMTPLine(t, w, "EHLO client.example")
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "MAIL FROM:<alice@example.com> BODY=8BITMIME")
	_, mailCode := readSMTPResponse(t, r)
	if mailCode != 250 {
		t.Fatalf("mail code=%d want=250", mailCode)
	}
	mustWriteSMTPLine(t, w, "RCPT TO:<bob@example.net>")
	_, rcptCode := readSMTPResponse(t, r)
	if rcptCode != 250 {
		t.Fatalf("rcpt code=%d want=250", rcptCode)
	}
	mustWriteSMTPLine(t, w, "DATA")
	_, dataCode := readSMTPResponse(t, r)
	if dataCode != 354 {
		t.Fatalf("data prompt code=%d want=354", dataCode)
	}
	if _, err := w.WriteString("Subject: trace\r\n\r\nhello\r\n.\r\n"); err != nil {
		t.Fatalf("write data: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush data: %v", err)
	}
	_, queuedCode := readSMTPResponse(t, r)
	if queuedCode != 250 {
		t.Fatalf("queued code=%d want=250", queuedCode)
	}
	mustWriteSMTPLine(t, w, "QUIT")
	_, quitCode := readSMTPResponse(t, r)
	if quitCode != 221 {
		t.Fatalf("quit code=%d want=221", quitCode)
	}

	spans := waitForSpans(t, exp, "smtp.session", "smtp.mail", "smtp.rcpt", "smtp.data")
	session := requireSpan(t, spans, "smtp.session")
	mail := requireSpan(t, spans, "smtp.mail")
	rcpt := requireSpan(t, spans, "smtp.rcpt")
	data := requireSpan(t, spans, "smtp.data")

	if mail.Parent.SpanID() != session.SpanContext.SpanID() {
		t.Fatalf("mail parent=%s want session=%s", mail.Parent.SpanID(), session.SpanContext.SpanID())
	}
	if rcpt.Parent.SpanID() != session.SpanContext.SpanID() {
		t.Fatalf("rcpt parent=%s want session=%s", rcpt.Parent.SpanID(), session.SpanContext.SpanID())
	}
	if data.Parent.SpanID() != session.SpanContext.SpanID() {
		t.Fatalf("data parent=%s want session=%s", data.Parent.SpanID(), session.SpanContext.SpanID())
	}
	if got := attrString(t, mail.Attributes, "smtp.mail_from"); got == "" {
		t.Fatal("smtp.mail_from should be recorded on smtp.mail span")
	}
	if got := attrString(t, rcpt.Attributes, "smtp.rcpt_to"); got == "" {
		t.Fatal("smtp.rcpt_to should be recorded on smtp.rcpt span")
	}
	if got := attrInt64(t, data.Attributes, "smtp.response.code"); got != 250 {
		t.Fatalf("smtp.data response code=%d want=250", got)
	}
	if got := attrInt64(t, data.Attributes, "smtp.data.bytes"); got <= 0 {
		t.Fatalf("smtp.data bytes=%d want > 0", got)
	}
	if got := attrString(t, data.Attributes, "smtp.auth.action"); got != string(mailauth.ActionAccept) {
		t.Fatalf("smtp.auth.action=%q want=%q", got, mailauth.ActionAccept)
	}
}

func TestSMTPTracingCapturesAuthSpan(t *testing.T) {
	exp := setupSMTPTraceExporter(t)
	authBackend, err := userauth.NewStatic("alice@example.com:s3cr3t")
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	s := NewSubmissionServer(
		config.Config{
			Hostname:           "mx.example.test",
			SubmissionAddr:     "127.0.0.1:587",
			SubmissionAuth:     true,
			SubmissionSenderID: true,
		},
		&recordingQueue{},
		nil,
		authBackend,
	)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)

	mustWriteSMTPLine(t, w, "EHLO client.example")
	_, _ = readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "AUTH PLAIN AGFsaWNlQGV4YW1wbGUuY29tAHMzY3IzdA==")
	_, code := readSMTPResponse(t, r)
	if code != 235 {
		t.Fatalf("auth code=%d want=235", code)
	}
	mustWriteSMTPLine(t, w, "QUIT")
	_, quitCode := readSMTPResponse(t, r)
	if quitCode != 221 {
		t.Fatalf("quit code=%d want=221", quitCode)
	}

	auth := requireSpan(t, waitForSpans(t, exp, "smtp.auth"), "smtp.auth")
	if got := attrString(t, auth.Attributes, "smtp.auth.mechanism"); got != "PLAIN" {
		t.Fatalf("mechanism=%q want=PLAIN", got)
	}
	if got := attrString(t, auth.Attributes, "smtp.auth.user"); got == "" {
		t.Fatal("smtp.auth.user should be recorded on smtp.auth span")
	}
	if got := attrBool(t, auth.Attributes, "smtp.auth.success"); !got {
		t.Fatal("smtp.auth.success should be true")
	}
	if got := attrInt64(t, auth.Attributes, "smtp.response.code"); got != 235 {
		t.Fatalf("smtp.auth response code=%d want=235", got)
	}
}

func TestSMTPTracingCapturesStartTLSSpan(t *testing.T) {
	exp := setupSMTPTraceExporter(t)
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatalf("selfSignedCert: %v", err)
	}
	s := &Server{
		cfg:       config.Config{Hostname: "mx.example.test"},
		tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r)

	mustWriteSMTPLine(t, w, "EHLO client.example")
	resp, code := readSMTPResponse(t, r)
	if code != 250 {
		t.Fatalf("ehlo code=%d want=250", code)
	}
	if resp == "" {
		t.Fatal("ehlo response should not be empty")
	}
	mustWriteSMTPLine(t, w, "STARTTLS")
	_, starttlsCode := readSMTPResponse(t, r)
	if starttlsCode != 220 {
		t.Fatalf("starttls code=%d want=220", starttlsCode)
	}

	tlsClient := tls.Client(client, &tls.Config{InsecureSkipVerify: true})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("tls handshake: %v", err)
	}
	rt := bufio.NewReader(tlsClient)
	wt := bufio.NewWriter(tlsClient)
	mustWriteSMTPLine(t, wt, "QUIT")
	_, quitCode := readSMTPResponse(t, rt)
	if quitCode != 221 {
		t.Fatalf("quit code=%d want=221", quitCode)
	}
	_ = tlsClient.Close()

	starttls := requireSpan(t, waitForSpans(t, exp, "smtp.starttls"), "smtp.starttls")
	if got := attrBool(t, starttls.Attributes, "smtp.tls"); !got {
		t.Fatal("smtp.tls should be true after STARTTLS")
	}
	if got := attrInt64(t, starttls.Attributes, "smtp.response.code"); got != 220 {
		t.Fatalf("smtp.starttls response code=%d want=220", got)
	}
}

func setupSMTPTraceExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	prevTracer := smtpTracer
	otel.SetTracerProvider(tp)
	smtpTracer = tp.Tracer("github.com/tamago0224/kuroshio-mta/internal/smtp")
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
		smtpTracer = prevTracer
	})
	return exp
}

func requireSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	var names []string
	for _, span := range spans {
		names = append(names, span.Name)
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q not found; got=%v", name, names)
	return tracetest.SpanStub{}
}

func waitForSpans(t *testing.T, exp *tracetest.InMemoryExporter, names ...string) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		spans := exp.GetSpans()
		if hasSpanNames(spans, names...) {
			return spans
		}
		if time.Now().After(deadline) {
			return spans
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasSpanNames(spans tracetest.SpanStubs, names ...string) bool {
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

func attrString(t *testing.T, attrs []attribute.KeyValue, key string) string {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return ""
}

func attrInt64(t *testing.T, attrs []attribute.KeyValue, key string) int64 {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsInt64()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return 0
}

func attrBool(t *testing.T, attrs []attribute.KeyValue, key string) bool {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsBool()
		}
	}
	t.Fatalf("attribute %q not found", key)
	return false
}
