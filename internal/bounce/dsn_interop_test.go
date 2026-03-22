package bounce

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestParseDSN_RFC3464StyleFailureSample(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"Reporting-MTA: dns; mx.example.org",
		"Arrival-Date: Wed, 11 Mar 2026 10:00:00 +0000",
		"",
		"Final-Recipient: rfc822; user@example.net",
		"Action: failed",
		"Status: 5.1.1",
		"Remote-MTA: dns; mx.target.example.net",
		"Diagnostic-Code: smtp; 550 5.1.1 User unknown",
		"",
	}, "\r\n"))

	reports, err := ParseDSN(raw)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len(reports)=%d, want 1", len(reports))
	}
	got := reports[0]
	if got.ReportingMTA != "mx.example.org" {
		t.Fatalf("reporting mta=%q", got.ReportingMTA)
	}
	if got.Recipient != "user@example.net" || got.Action != "failed" || got.Status != "5.1.1" {
		t.Fatalf("unexpected report: %+v", got)
	}
}

func TestParseDSN_RFC3464StyleDelayedSample(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"Reporting-MTA: dns; mx.example.org",
		"Arrival-Date: Wed, 11 Mar 2026 10:00:00 +0000",
		"",
		"Final-Recipient: rfc822; user@example.net",
		"Action: delayed",
		"Status: 4.4.1",
		"Will-Retry-Until: Wed, 11 Mar 2026 22:00:00 +0000",
		"Diagnostic-Code: smtp; 451 4.4.1 temporary local problem",
		"",
	}, "\r\n"))

	reports, err := ParseDSN(raw)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len(reports)=%d, want 1", len(reports))
	}
	got := reports[0]
	if got.Action != "delayed" || got.Status != "4.4.1" {
		t.Fatalf("unexpected report: %+v", got)
	}
	if got.WillRetryUntil == "" {
		t.Fatalf("will-retry-until must be parsed: %+v", got)
	}
}

func TestParseDSN_InteroperatesWithGeneratedFailureDSN(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	orig := &model.Message{
		ID:       "m1",
		MailFrom: "sender@example.com",
		Data: []byte(strings.Join([]string{
			"From: sender@example.com",
			"To: user@example.net",
			"Subject: hello",
			"",
			"payload",
		}, "\r\n")),
	}
	generated, err := BuildFailureDSN(orig, "user@example.net", "550 5.1.1 User unknown", "mx.example.com", now)
	if err != nil {
		t.Fatalf("BuildFailureDSN: %v", err)
	}
	statusPart, err := extractDeliveryStatusPart(generated.Data)
	if err != nil {
		t.Fatalf("extractDeliveryStatusPart: %v", err)
	}
	reports, err := ParseDSN(statusPart)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len(reports)=%d, want 1", len(reports))
	}
	got := reports[0]
	if got.Action != "failed" || got.Status != "5.1.1" {
		t.Fatalf("unexpected report: %+v", got)
	}
	if got.Recipient != "user@example.net" || got.ReportingMTA != "mx.example.com" {
		t.Fatalf("unexpected report: %+v", got)
	}
}

var boundaryRe = regexp.MustCompile(`(?i)boundary="([^"]+)"`)

func extractDeliveryStatusPart(raw []byte) ([]byte, error) {
	msg := string(raw)
	m := boundaryRe.FindStringSubmatch(msg)
	if len(m) != 2 {
		return nil, fmt.Errorf("boundary not found")
	}
	boundary := m[1]
	parts := strings.Split(msg, "--"+boundary)
	for _, part := range parts {
		if !strings.Contains(strings.ToLower(part), "content-type: message/delivery-status") {
			continue
		}
		idx := strings.Index(part, "\r\n\r\n")
		sepLen := 4
		if idx < 0 {
			idx = strings.Index(part, "\n\n")
			sepLen = 2
		}
		if idx < 0 {
			return nil, fmt.Errorf("delivery-status body separator not found")
		}
		out := strings.TrimSpace(part[idx+sepLen:])
		if out == "" {
			return nil, fmt.Errorf("delivery-status part is empty")
		}
		return []byte(out), nil
	}
	return nil, fmt.Errorf("delivery-status part not found")
}
