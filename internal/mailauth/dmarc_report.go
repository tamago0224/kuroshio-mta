package mailauth

import (
	"fmt"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/util"
)

type DMARCOutboundReport struct {
	To      string
	Subject string
	Body    []byte
}

func BuildDMARCOutboundReports(res DMARCResult, fromDomain, hostname, msgID string, now time.Time) []DMARCOutboundReport {
	out := make([]DMARCOutboundReport, 0, len(res.AggregateReport)+len(res.FailureReport))
	for _, rua := range res.AggregateReport {
		to, ok := parseDMARCReportURI(rua)
		if !ok {
			continue
		}
		body := strings.Join([]string{
			"DMARC Aggregate Report",
			fmt.Sprintf("domain: %s", fromDomain),
			fmt.Sprintf("result: %s", res.Result),
			fmt.Sprintf("policy: %s", res.Policy),
			fmt.Sprintf("message-id: %s", msgID),
			fmt.Sprintf("time: %s", now.UTC().Format(time.RFC3339)),
			"",
		}, "\n")
		out = append(out, DMARCOutboundReport{
			To:      to,
			Subject: fmt.Sprintf("DMARC aggregate report for %s", fromDomain),
			Body:    []byte(body),
		})
	}
	if !strings.EqualFold(res.Result, "fail") {
		return out
	}
	for _, ruf := range res.FailureReport {
		to, ok := parseDMARCReportURI(ruf)
		if !ok {
			continue
		}
		body := strings.Join([]string{
			"DMARC Failure Report",
			fmt.Sprintf("domain: %s", fromDomain),
			fmt.Sprintf("result: %s", res.Result),
			fmt.Sprintf("policy: %s", res.Policy),
			fmt.Sprintf("reason: %s", res.Reason),
			fmt.Sprintf("message-id: %s", msgID),
			fmt.Sprintf("reporter: %s", hostname),
			fmt.Sprintf("time: %s", now.UTC().Format(time.RFC3339)),
			"",
		}, "\n")
		out = append(out, DMARCOutboundReport{
			To:      to,
			Subject: fmt.Sprintf("DMARC failure report for %s", fromDomain),
			Body:    []byte(body),
		})
	}
	return out
}

func parseDMARCReportURI(v string) (string, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(s), "mailto:") {
		addr := strings.TrimSpace(s[len("mailto:"):])
		if i := strings.Index(addr, "!"); i >= 0 {
			addr = strings.TrimSpace(addr[:i])
		}
		norm, err := util.NormalizePath(addr)
		if err != nil || norm == "" {
			return "", false
		}
		return norm, true
	}
	return "", false
}
