package smtp

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/dmarcreport"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

var dmarcReportOrganizationalDomain = mailauth.DMARCbisOrganizationalDomain
var dmarcReportLookupTXT = net.DefaultResolver.LookupTXT

func (s *Server) recordDMARCObservation(ctx context.Context, authRes mailauth.Result, mailFrom string, rcpts []string, remoteIP net.IP, raw []byte, now time.Time) {
	if s.dmarcReports == nil || s.submission || s.cfg.DMARCPolicyMode != "enforce" || authRes.DMARC.DMARCbis == nil || hasAggregateReportMarker(raw) {
		return
	}
	result := authRes.DMARC.DMARCbis
	if result.PolicyDomain == "" || len(result.AggregateReport) == 0 || !strings.EqualFold(result.Result, "pass") && !strings.EqualFold(result.Result, "fail") {
		return
	}
	policyOrg := result.OrgDomain
	if policyOrg == "" {
		return
	}
	var recipients []string
	lookupCtx, cancelLookup := context.WithTimeout(ctx, 4*time.Second)
	defer cancelLookup()
	for _, uri := range result.AggregateReport {
		to, ok := parseDMARCReportRecipient(uri)
		if !ok {
			continue
		}
		destination, ok := util.DomainOf(to)
		if !ok {
			continue
		}
		destinationOrg, err := dmarcReportOrganizationalDomain(lookupCtx, destination)
		if err == nil {
			var authorized bool
			authorized, err = dmarcreport.AuthorizeExternalDestination(lookupCtx, result.PolicyDomain, policyOrg, destination, destinationOrg, dmarcReportLookupTXT)
			if authorized && err == nil {
				recipients = append(recipients, uri)
			} else if err == nil {
				s.metricInc("dmarc_aggregate_destination_rejected_total")
			}
		}
		if err != nil {
			s.metricInc("dmarc_aggregate_destination_errors_total")
			slog.Warn("DMARC report destination authorization lookup failed", "component", "smtp", "error", err, "policy_domain", result.PolicyDomain)
			if lookupCtx.Err() != nil {
				break
			}
		}
	}
	if len(recipients) == 0 {
		return
	}
	fromDomain, _ := util.DomainOf(mailFrom)
	disposition := "none"
	if strings.EqualFold(result.Result, "fail") {
		switch strings.ToLower(result.Policy) {
		case "reject":
			disposition = "reject"
		case "quarantine":
			disposition = "quarantine"
		}
	}
	spfAligned, dkimAligned := "fail", "fail"
	if result.SPFAligned {
		spfAligned = "pass"
	}
	if result.DKIMAligned {
		dkimAligned = "pass"
	}
	policy := dmarcreport.Policy{P: result.PublishedP, SP: result.PublishedSP, NP: result.PublishedNP, FO: result.FailureOptions, ADKIM: result.DKIMMode, ASPF: result.SPFMode, Testing: result.Testing}
	if policy.P == "" {
		policy.P = result.Policy
	}
	if policy.P != "none" && policy.P != "quarantine" && policy.P != "reject" {
		policy.P = "none"
	}
	var dkimResults []dmarcreport.DKIMResult
	for _, sig := range authRes.DKIM.Sigs {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(sig.Domain), "."))
		if domain == "" {
			continue
		}
		dkimResults = append(dkimResults, dmarcreport.DKIMResult{Domain: domain, Selector: strings.ToLower(sig.Selector), Result: sig.Result})
	}
	// RFC 9990 envelope identifiers are domains. Leave envelope_to empty when
	// multiple recipient domains are present rather than persisting mailbox data.
	envelopeTo := ""
	var domains []string
	for _, rcpt := range rcpts {
		if domain, ok := util.DomainOf(rcpt); ok {
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)
	if len(domains) == 1 {
		envelopeTo = domains[0]
	}
	sourceIP := remoteIP.String()
	if sourceIP == "" {
		return
	}
	ob := dmarcreport.Observation{ObservedAt: now, PolicyDomain: result.PolicyDomain, AuthorDomain: result.AuthorDomain, Policy: policy, Discovery: result.Discovery, SourceIP: sourceIP, Disposition: disposition, DKIMAligned: dkimAligned, SPFAligned: spfAligned, EnvelopeFrom: fromDomain, EnvelopeTo: envelopeTo, DKIMResults: dkimResults, SPFDomain: authRes.SPF.Domain, SPFResult: authRes.SPF.Result, Recipients: recipients}
	if err := s.dmarcReports.Add(ctx, ob); err != nil {
		s.metricInc("dmarc_aggregate_observation_errors_total")
		slog.Warn("DMARC aggregate observation failed", "component", "smtp", "error", err, "policy_domain", result.PolicyDomain)
	} else {
		s.metricInc("dmarc_aggregate_observations_total")
	}
}

func hasAggregateReportMarker(raw []byte) bool {
	headerPart, _, err := mailauth.SplitMessage(raw)
	if err != nil {
		return false
	}
	headers, err := mailauth.ParseHeaders(headerPart)
	if err != nil {
		return false
	}
	for _, header := range headers {
		if strings.EqualFold(header.Name, "X-Kuroshio-DMARC-Report") && strings.EqualFold(strings.TrimSpace(header.Value), "aggregate") {
			return true
		}
	}
	return false
}

func parseDMARCReportRecipient(uri string) (string, bool) {
	value := strings.TrimSpace(uri)
	if len(value) < len("mailto:") || !strings.EqualFold(value[:len("mailto:")], "mailto:") {
		return "", false
	}
	address := strings.TrimSpace(value[len("mailto:"):])
	if i := strings.IndexByte(address, '!'); i >= 0 {
		address = strings.TrimSpace(address[:i])
	}
	normalized, err := util.NormalizePath(address)
	return normalized, err == nil && normalized != ""
}
