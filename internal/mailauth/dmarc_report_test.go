package mailauth

import (
	"testing"
	"time"
)

func TestBuildDMARCOutboundReportsFailIncludesAggregateAndFailure(t *testing.T) {
	now := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)
	res := DMARCResult{
		Result:          "fail",
		Policy:          "reject",
		Reason:          "alignment failed",
		AggregateReport: []string{"mailto:agg@example.net", "https://ignored.example.net"},
		FailureReport:   []string{"mailto:forensic@example.net!10m"},
	}

	got := BuildDMARCOutboundReports(res, "example.com", "mx.example.test", "msg-1", now)
	if len(got) != 2 {
		t.Fatalf("len=%d want=2", len(got))
	}
	if got[0].To != "agg@example.net" {
		t.Fatalf("aggregate to=%q", got[0].To)
	}
	if got[1].To != "forensic@example.net" {
		t.Fatalf("failure to=%q", got[1].To)
	}
}

func TestBuildDMARCOutboundReportsPassSkipsFailure(t *testing.T) {
	now := time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC)
	res := DMARCResult{
		Result:          "pass",
		Policy:          "none",
		AggregateReport: []string{"mailto:agg@example.net"},
		FailureReport:   []string{"mailto:forensic@example.net"},
	}

	got := BuildDMARCOutboundReports(res, "example.com", "mx.example.test", "msg-1", now)
	if len(got) != 1 {
		t.Fatalf("len=%d want=1", len(got))
	}
	if got[0].To != "agg@example.net" {
		t.Fatalf("to=%q", got[0].To)
	}
}

func TestParseDMARCReportURI(t *testing.T) {
	tests := []struct {
		in string
		to string
		ok bool
	}{
		{in: "mailto:agg@example.net", to: "agg@example.net", ok: true},
		{in: "mailto:<Agg@Example.NET>", to: "agg@example.net", ok: true},
		{in: "mailto:forensic@example.net!50k", to: "forensic@example.net", ok: true},
		{in: "https://example.net", ok: false},
		{in: "mailto:bad address@example.net", ok: false},
		{in: "mailto:", ok: false},
	}

	for _, tt := range tests {
		gotTo, gotOK := parseDMARCReportURI(tt.in)
		if gotTo != tt.to || gotOK != tt.ok {
			t.Fatalf("in=%q got=(%q,%v) want=(%q,%v)", tt.in, gotTo, gotOK, tt.to, tt.ok)
		}
	}
}
