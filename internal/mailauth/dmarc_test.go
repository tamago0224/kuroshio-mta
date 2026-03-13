package mailauth

import (
	"context"
	"reflect"
	"testing"
)

func TestEvalDMARCParsesMajorTags(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})

	dmarcLookupTXT = func(_ context.Context, name string) ([]string, error) {
		return []string{"v=DMARC1; p=reject; sp=quarantine; pct=25; fo=1:d:s; rf=afrf:iodef; ri=3600; rua=mailto:agg@example.com,mailto:agg2@example.com; ruf=mailto:fail@example.com"}, nil
	}

	res := EvalDMARC("example.com", SPFResult{Result: "pass", Domain: "example.com"}, DKIMResult{})
	if res.Result != "pass" {
		t.Fatalf("result=%q", res.Result)
	}
	if res.Policy != "reject" || res.SubdomainPolicy != "quarantine" {
		t.Fatalf("policy=%q sp=%q", res.Policy, res.SubdomainPolicy)
	}
	if res.Percent != 25 || res.ReportInterval != 3600 {
		t.Fatalf("pct=%d ri=%d", res.Percent, res.ReportInterval)
	}
	if !reflect.DeepEqual(res.FailureOptions, []string{"1:d:s"}) {
		t.Fatalf("fo=%v", res.FailureOptions)
	}
	if !reflect.DeepEqual(res.ReportFormat, []string{"afrf:iodef"}) {
		t.Fatalf("rf=%v", res.ReportFormat)
	}
	if !reflect.DeepEqual(res.AggregateReport, []string{"mailto:agg@example.com", "mailto:agg2@example.com"}) {
		t.Fatalf("rua=%v", res.AggregateReport)
	}
	if !reflect.DeepEqual(res.FailureReport, []string{"mailto:fail@example.com"}) {
		t.Fatalf("ruf=%v", res.FailureReport)
	}
}

func TestEvalDMARCDefaultsMajorTags(t *testing.T) {
	origLookup := dmarcLookupTXT
	t.Cleanup(func() {
		dmarcLookupTXT = origLookup
	})

	dmarcLookupTXT = func(_ context.Context, name string) ([]string, error) {
		return []string{"v=DMARC1; p=none"}, nil
	}

	res := EvalDMARC("example.com", SPFResult{Result: "fail", Domain: "other.example"}, DKIMResult{})
	if res.Policy != "none" || res.SubdomainPolicy != "none" {
		t.Fatalf("policy=%q sp=%q", res.Policy, res.SubdomainPolicy)
	}
	if res.Percent != 100 || res.ReportInterval != 86400 {
		t.Fatalf("pct=%d ri=%d", res.Percent, res.ReportInterval)
	}
	if !reflect.DeepEqual(res.FailureOptions, []string{"0"}) {
		t.Fatalf("fo=%v", res.FailureOptions)
	}
	if !reflect.DeepEqual(res.ReportFormat, []string{"afrf"}) {
		t.Fatalf("rf=%v", res.ReportFormat)
	}
	if len(res.AggregateReport) != 0 || len(res.FailureReport) != 0 {
		t.Fatalf("rua=%v ruf=%v", res.AggregateReport, res.FailureReport)
	}
}

func TestParseDMARCHelpers(t *testing.T) {
	if got := parseDMARCInt("42", 1); got != 42 {
		t.Fatalf("parseDMARCInt=%d", got)
	}
	if got := parseDMARCInt("bad", 1); got != 1 {
		t.Fatalf("parseDMARCInt fallback=%d", got)
	}
	if got := parseDMARCList(" a , b ", nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("parseDMARCList=%v", got)
	}
	if got := parseDMARCList("", []string{"x"}); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("parseDMARCList fallback=%v", got)
	}
}
