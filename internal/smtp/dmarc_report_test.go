package smtp

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/dmarcreport"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

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
	s := &Server{cfg: config.Config{DMARCPolicyMode: "shadow"}, dmarcReports: store, metrics: observability.NewMetrics()}
	now := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	auth := mailauth.Result{SPF: mailauth.SPFResult{Domain: "bounce.example.test", Result: "pass"}, DMARC: mailauth.DMARCResult{DMARCbis: &mailauth.DMARCbisResult{
		Result: "fail", AuthorDomain: "mail.example.test", PolicyDomain: "example.test", OrgDomain: "example.test", Policy: "reject", PublishedP: "reject", Discovery: "treewalk", AggregateReport: []string{"mailto:agg@reports.vendor.test"},
	}}}
	s.recordDMARCObservation(context.Background(), auth, "sender@example.test", []string{"user@recipient.test"}, net.ParseIP("192.0.2.8"), []byte("From: sender@example.test\r\n\r\nbody\r\n"), now)
	rows, err := store.Snapshot(context.Background(), now)
	if err != nil || len(rows) != 0 {
		t.Fatalf("shadow observation stored: rows=%+v err=%v", rows, err)
	}
	s.cfg.DMARCPolicyMode = "enforce"
	s.recordDMARCObservation(context.Background(), auth, "sender@example.test", []string{"user@recipient.test"}, net.ParseIP("192.0.2.8"), []byte("From: sender@example.test\r\n\r\nbody\r\n"), now)
	rows, err = store.Snapshot(context.Background(), now)
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
