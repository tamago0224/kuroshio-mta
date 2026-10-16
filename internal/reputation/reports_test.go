package reputation

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testTLSReport(now time.Time) []byte {
	return []byte(fmt.Sprintf(`{"organization-name":"sender.example","report-id":"report-1","date-range":{"start-datetime":%q,"end-datetime":%q},"policies":[{"policy":{"policy-domain":"Example.COM"},"summary":{"total-successful-session-count":10,"total-failure-session-count":2}}]}`, now.Add(-24*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339)))
}

const testComplaint = "Feedback-Type: abuse\r\nVersion: 1\r\nUser-Agent: collector/1\r\nOriginal-Rcpt-To: rfc822; user@example.net\r\nIncidents: 2\r\n"

func TestParseTLSReport(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	raw := testTLSReport(now)
	report, err := ParseTLSReport(raw, "application/tlsrpt+json", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["example.com"] != (ReportCounts{Successes: 10, Failures: 2}) {
		t.Fatalf("unexpected counts: %+v", report.Counts)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	zipped, err := ParseTLSReport(compressed.Bytes(), "application/tlsrpt+gzip", now)
	if err != nil || zipped.ID != report.ID {
		t.Fatalf("gzip identity: %+v %v", zipped, err)
	}
	tests := []struct{ name, raw string }{
		{name: "malformed", raw: "{"},
		{name: "missing id", raw: strings.Replace(string(raw), `"report-1"`, `""`, 1)},
		{name: "negative count", raw: strings.Replace(string(raw), `"total-failure-session-count":2`, `"total-failure-session-count":-1`, 1)},
		{name: "missing count", raw: strings.Replace(string(raw), `,"total-failure-session-count":2`, "", 1)},
		{name: "invalid domain", raw: strings.Replace(string(raw), "Example.COM", "bad@domain", 1)},
		{name: "invalid period", raw: strings.Replace(string(raw), now.Add(-24*time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339), 1)},
		{name: "stale period", raw: strings.Replace(string(raw), now.Add(-24*time.Hour).Format(time.RFC3339), now.Add(-31*24*time.Hour).Format(time.RFC3339), 1)},
		{name: "oversized", raw: strings.Repeat("x", MaxReportBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseTLSReport([]byte(tt.raw), "application/tlsrpt+json", now); err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
	compressed.Reset()
	writer = gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("x"), MaxReportBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTLSReport(compressed.Bytes(), "application/tlsrpt+gzip", now); err == nil {
		t.Fatal("gzip expansion limit not enforced")
	}
}

func TestParseComplaint(t *testing.T) {
	report, err := ParseComplaint([]byte(testComplaint), "message/feedback-report", "provider/123")
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["example.net"].Complaints != 2 {
		t.Fatalf("counts: %+v", report.Counts)
	}
	multipart := "--part\r\nContent-Type: text/plain\r\n\r\nHuman text\r\n--part\r\nContent-Type: message/feedback-report\r\n\r\n" + testComplaint + "\r\n--part--\r\n"
	multi, err := ParseComplaint([]byte(multipart), `multipart/report; report-type=feedback-report; boundary=part`, "provider/123")
	if err != nil || multi.ID != report.ID {
		t.Fatalf("multipart: %+v %v", multi, err)
	}
	tests := []struct{ name, raw, content, id string }{
		{name: "no id", raw: testComplaint, content: "message/feedback-report"},
		{name: "wrong feedback", raw: strings.Replace(testComplaint, "abuse", "fraud", 1), content: "message/feedback-report", id: "1"},
		{name: "missing recipient", raw: "Feedback-Type: abuse\r\nVersion: 1\r\nUser-Agent: collector\r\n", content: "message/feedback-report", id: "1"},
		{name: "bad recipient", raw: strings.Replace(testComplaint, "user@example.net", "not-an-address", 1), content: "message/feedback-report", id: "1"},
		{name: "bad incidents", raw: strings.Replace(testComplaint, "Incidents: 2", "Incidents: -1", 1), content: "message/feedback-report", id: "1"},
		{name: "bad mime", raw: multipart, content: "multipart/report", id: "1"},
		{name: "wrong content", raw: testComplaint, content: "text/plain", id: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseComplaint([]byte(tt.raw), tt.content, tt.id); err == nil {
				t.Fatal("invalid complaint accepted")
			}
		})
	}
}
