package mailauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type dmarcbisRecord struct {
	domain string
	tags   map[string]string
}

// dmarcBisLookupTXT is independent from the legacy resolver for shadow tests.
var dmarcBisLookupTXT = func(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

var dmarcBisDomainExists = func(ctx context.Context, domain string) (bool, error) {
	_, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
	if err == nil {
		return true, nil
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return false, nil
	}
	return false, err
}

// EvalDMARCbis evaluates RFC 9989 as an observation. Its result must not be
// used to change the current delivery action until migration is explicitly enabled.
func EvalDMARCbis(ctx context.Context, author string, spf SPFResult, dkim DKIMResult) DMARCbisResult {
	author = normalizeDMARCDomain(author)
	if author == "" {
		return DMARCbisResult{Result: "permerror", Reason: "missing Author Domain"}
	}
	records, err := dmarcBisTreeWalk(ctx, author)
	if err != nil {
		return DMARCbisResult{Result: "temperror", AuthorDomain: author, Reason: "DNS lookup failed"}
	}
	if len(records) == 0 {
		return DMARCbisResult{Result: "none", AuthorDomain: author, OrgDomain: author, Reason: "no applicable policy"}
	}
	rec := records[0]
	org := dmarcBisOrgFromRecords(author, records)
	policy := rec.tags["p"]
	if rec.domain != author {
		exists, err := dmarcBisDomainExists(ctx, author)
		if err != nil {
			return DMARCbisResult{Result: "temperror", AuthorDomain: author, PolicyDomain: rec.domain, OrgDomain: org, Reason: "Author Domain existence lookup failed"}
		}
		key := "np"
		if exists {
			key = "sp"
		}
		if value := rec.tags[key]; value != "" {
			policy = value
		}
	}
	pass := false
	aspf, adkim := rec.tags["aspf"], rec.tags["adkim"]
	if aspf == "" {
		aspf = "r"
	}
	if adkim == "" {
		adkim = "r"
	}
	if strings.EqualFold(spf.Result, "pass") {
		aligned, err := dmarcBisAligned(ctx, author, spf.Domain, org, aspf)
		if err != nil {
			return DMARCbisResult{Result: "temperror", AuthorDomain: author, PolicyDomain: rec.domain, OrgDomain: org, Policy: policy, Discovery: "treewalk", Reason: "identifier alignment lookup failed"}
		}
		pass = aligned
	}
	for _, sig := range dkim.Sigs {
		if strings.EqualFold(sig.Result, "pass") {
			aligned, err := dmarcBisAligned(ctx, author, sig.Domain, org, adkim)
			if err != nil {
				return DMARCbisResult{Result: "temperror", AuthorDomain: author, PolicyDomain: rec.domain, OrgDomain: org, Policy: policy, Discovery: "treewalk", Reason: "identifier alignment lookup failed"}
			}
			if aligned {
				pass = true
				break
			}
		}
	}
	result, reason := "fail", "alignment failed"
	if pass {
		result, reason = "pass", ""
	}
	if rec.tags["t"] == "n" {
		policy, reason = "none", "assessment policy disabled by t=n"
	}
	return DMARCbisResult{Result: result, AuthorDomain: author, PolicyDomain: rec.domain, OrgDomain: org, Policy: policy, Discovery: "treewalk", Reason: reason}
}

func dmarcBisAligned(ctx context.Context, author, identifier, org, mode string) (bool, error) {
	identifier = normalizeDMARCDomain(identifier)
	if identifier == "" {
		return false, nil
	}
	if mode == "s" {
		return identifier == author, nil
	}
	identifierOrg, err := dmarcBisOrg(ctx, identifier)
	return identifierOrg == org, err
}

func dmarcBisOrg(ctx context.Context, domain string) (string, error) {
	domain = normalizeDMARCDomain(domain)
	records, err := dmarcBisTreeWalk(ctx, domain)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return domain, nil
	}
	return dmarcBisOrgFromRecords(domain, records), nil
}

func dmarcBisOrgFromRecords(domain string, records []dmarcbisRecord) string {
	for _, record := range records {
		if record.tags["psd"] == "n" {
			return record.domain
		}
		if record.domain != domain && record.tags["psd"] == "y" {
			return labelChild(domain, record.domain)
		}
	}
	return records[len(records)-1].domain
}

func dmarcBisTreeWalk(ctx context.Context, domain string) ([]dmarcbisRecord, error) {
	domain = normalizeDMARCDomain(domain)
	if domain == "" {
		return nil, nil
	}
	labels := strings.Split(domain, ".")
	if len(labels) > 127 {
		return nil, fmt.Errorf("invalid Author Domain")
	}
	var records []dmarcbisRecord
	query := func(name string) (bool, error) {
		answers, err := dmarcBisLookupTXT(ctx, "_dmarc."+name)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return false, nil
			}
			return false, err
		}
		valid := make([]map[string]string, 0, 1)
		for _, answer := range answers {
			if tags, ok := parseDMARCbisRecord(answer); ok {
				valid = append(valid, tags)
			}
		}
		if len(valid) != 1 {
			return false, nil
		}
		tags := valid[0]
		records = append(records, dmarcbisRecord{domain: name, tags: tags})
		return tags["psd"] == "y" || tags["psd"] == "n", nil
	}
	stop, err := query(domain)
	if err != nil || stop {
		return records, err
	}
	start := 1 // The immediate parent is the first Tree Walk target.
	if len(labels) > 8 {
		start = len(labels) - 7
	}
	queries := 1
	for i := start; i < len(labels) && queries < 8; i++ {
		stop, err := query(strings.Join(labels[i:], "."))
		if err != nil {
			return nil, err
		}
		queries++
		if stop {
			break
		}
	}
	return records, nil
}

func parseDMARCbisRecord(raw string) (map[string]string, bool) {
	parts := strings.Split(raw, ";")
	tags := make(map[string]string, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.ToLower(strings.TrimSpace(value))
		if !ok || key == "" || value == "" || strings.ContainsAny(key, " \t") {
			return nil, false
		}
		if _, exists := tags[key]; exists {
			return nil, false
		}
		if i == 0 && key != "v" {
			return nil, false
		}
		tags[key] = value
	}
	if tags["v"] != "dmarc1" {
		return nil, false
	}
	validPolicy := func(v string) bool { return v == "none" || v == "quarantine" || v == "reject" }
	if !validPolicy(tags["p"]) {
		return nil, false
	}
	for _, key := range []string{"sp", "np"} {
		if v, ok := tags[key]; ok && !validPolicy(v) {
			return nil, false
		}
	}
	for _, key := range []string{"psd", "t"} {
		if v, ok := tags[key]; ok && v != "y" && v != "n" {
			return nil, false
		}
	}
	for _, key := range []string{"aspf", "adkim"} {
		if v, ok := tags[key]; ok && v != "r" && v != "s" {
			return nil, false
		}
	}
	return tags, true
}

func normalizeDMARCDomain(domain string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func labelChild(subject, parent string) string {
	if subject == parent || !strings.HasSuffix(subject, "."+parent) {
		return parent
	}
	child := strings.TrimSuffix(subject, "."+parent)
	if _, _, ok := strings.Cut(child, "."); !ok {
		return subject
	}
	return child + "." + parent
}
