package tlsreport

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
)

func TestManagerPersistsBuildsAndDeliversDailyReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := testConfig()
	m, err := Open(cfg, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	period := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	observations := []delivery.TLSObservation{
		{ObservedAt: period, PolicyDomain: "example.test", PolicyType: "sts", PolicyString: []string{"version: STSv1", "mode: enforce"}, MXHost: "mx.example.test", Success: true},
		{ObservedAt: period.Add(time.Hour), PolicyDomain: "example.test", PolicyType: "sts", PolicyString: []string{"version: STSv1", "mode: enforce"}, MXHost: "mx.example.test", ResultType: "starttls-not-supported", Reason: "STARTTLS missing"},
	}
	for _, observation := range observations {
		if err := m.add(observation); err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := Open(cfg, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var delivered report
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/tlsrpt+json" {
			t.Errorf("content type=%q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	reopened.client = server.Client()
	reopened.lookupTXT = func(context.Context, string) ([]string, error) { return []string{"v=TLSRPTv1; rua=" + server.URL}, nil }
	reopened.now = func() time.Time { return period.Add(48 * time.Hour) }
	if err := reopened.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivered.Policies) != 1 || delivered.Policies[0].Summary.Success != 1 || delivered.Policies[0].Summary.Failure != 1 {
		t.Fatalf("unexpected report: %+v", delivered)
	}
	if len(reopened.state.Aggregates) != 0 {
		t.Fatalf("aggregates not acknowledged: %+v", reopened.state.Aggregates)
	}
	if filepath.Base(reopened.path) != "tls-report-state.json" {
		t.Fatal(reopened.path)
	}
}

func TestManagerRetainsDataOnDeliveryFailureAndRejectsAmbiguousPolicy(t *testing.T) {
	t.Parallel()
	m, err := Open(testConfig(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	period := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if err := m.add(delivery.TLSObservation{ObservedAt: period, PolicyDomain: "example.test", PolicyType: "tlsa", PolicyString: []string{"3 1 1 abcd"}, MXHost: "mx.example.test", ResultType: "validation-failure", Reason: "DANE failed"}); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return period.Add(48 * time.Hour) }
	m.lookupTXT = func(context.Context, string) ([]string, error) {
		return []string{"v=TLSRPTv1; rua=https://one.test", "v=TLSRPTv1; rua=https://two.test"}, nil
	}
	if err := m.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(m.state.Aggregates) != 1 {
		t.Fatalf("ambiguous policy deleted data: %+v", m.state.Aggregates)
	}
}

func TestUnsafeIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		value  string
		unsafe bool
	}{
		{name: "loopback", value: "127.0.0.1", unsafe: true},
		{name: "private", value: "10.0.0.1", unsafe: true},
		{name: "link local", value: "169.254.1.1", unsafe: true},
		{name: "documentation public", value: "192.0.2.1", unsafe: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := unsafeIP(net.ParseIP(tc.value)); got != tc.unsafe {
				t.Fatalf("unsafeIP(%s)=%t", tc.value, got)
			}
		})
	}
}

func TestFlushKeepsUTCPeriodsSeparate(t *testing.T) {
	t.Parallel()
	m, err := Open(testConfig(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, observed := range []time.Time{start, start.Add(24 * time.Hour)} {
		if err := m.add(delivery.TLSObservation{ObservedAt: observed, PolicyDomain: "example.test", PolicyType: "sts", PolicyString: []string{"version: STSv1", "mode: enforce", "mx: mx.example.test"}, MXHost: "mx.example.test", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	var starts []time.Time
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got report
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		starts = append(starts, got.DateRange.Start)
	}))
	defer server.Close()
	m.client = server.Client()
	m.lookupTXT = func(context.Context, string) ([]string, error) { return []string{"v=TLSRPTv1; rua=" + server.URL}, nil }
	m.now = func() time.Time { return start.Add(72 * time.Hour) }
	if err := m.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 || !starts[0].Equal(start) || !starts[1].Equal(start.Add(24*time.Hour)) {
		t.Fatalf("periods=%v", starts)
	}
}

func testConfig() config.TLSReportConfig {
	return config.TLSReportConfig{Enabled: true, Organization: "Kuroshio", ContactInfo: "postmaster@sender.test", ReportDomain: "sender.test", FlushInterval: time.Hour, MaxRecords: 100, MaxReportBytes: 1024 * 1024}
}
