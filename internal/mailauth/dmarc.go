package mailauth

import (
	"context"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/util"
)

var dmarcLookupTXT = func(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func EvalDMARC(fromDomain string, spf SPFResult, dkim DKIMResult) DMARCResult {
	if fromDomain == "" {
		return DMARCResult{Result: "permerror", Reason: "missing From domain"}
	}
	rec, policyDomain, ok, err := lookupDMARC(fromDomain)
	if err != nil {
		return DMARCResult{Domain: fromDomain, Result: "temperror", Reason: err.Error()}
	}
	if !ok {
		return DMARCResult{Domain: fromDomain, Result: "none", Policy: "none", SubdomainPolicy: "none", Percent: 100, ReportInterval: 86400, Reason: "no dmarc record"}
	}
	pol := parseTagList(rec)
	aspf := pol["aspf"]
	if aspf == "" {
		aspf = "r"
	}
	adkim := pol["adkim"]
	if adkim == "" {
		adkim = "r"
	}
	policy := strings.ToLower(pol["p"])
	if policy == "" {
		policy = "none"
	}
	subPolicy := strings.ToLower(pol["sp"])
	if subPolicy == "" {
		subPolicy = policy
	}
	effectivePolicy := policy
	if !strings.EqualFold(strings.TrimSpace(fromDomain), strings.TrimSpace(policyDomain)) {
		effectivePolicy = subPolicy
	}
	percent := parseDMARCInt(pol["pct"], 100)
	ri := parseDMARCInt(pol["ri"], 86400)
	fo := parseDMARCTokenList(pol["fo"], []string{"0"}, ":")
	rf := parseDMARCTokenList(pol["rf"], []string{"afrf"}, ":")
	rua := parseDMARCList(pol["rua"], nil)
	ruf := parseDMARCList(pol["ruf"], nil)

	spfAligned := strings.EqualFold(spf.Result, "pass") && aligned(fromDomain, spf.Domain, aspf)
	dkimAligned := false
	for _, s := range dkim.Sigs {
		if strings.EqualFold(s.Result, "pass") && aligned(fromDomain, s.Domain, adkim) {
			dkimAligned = true
			break
		}
	}
	if spfAligned || dkimAligned {
		return DMARCResult{
			Domain:          fromDomain,
			Result:          "pass",
			Policy:          effectivePolicy,
			SubdomainPolicy: subPolicy,
			Percent:         percent,
			FailureOptions:  fo,
			ReportFormat:    rf,
			ReportInterval:  ri,
			AggregateReport: rua,
			FailureReport:   ruf,
		}
	}
	return DMARCResult{
		Domain:          fromDomain,
		Result:          "fail",
		Policy:          effectivePolicy,
		SubdomainPolicy: subPolicy,
		Percent:         percent,
		FailureOptions:  fo,
		ReportFormat:    rf,
		ReportInterval:  ri,
		AggregateReport: rua,
		FailureReport:   ruf,
		Reason:          "alignment failed",
	}
}

func ExtractFromDomain(headers []Header) string {
	from, ok := FirstHeader(headers, "From")
	if !ok {
		return ""
	}
	addr := from
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			addr = from[i+1 : i+j]
		}
	}
	addr = strings.Trim(addr, " \t\"'")
	d, ok := util.DomainOf(addr)
	if !ok {
		return ""
	}
	return d
}

func aligned(fromDomain, authDomain, mode string) bool {
	if fromDomain == "" || authDomain == "" {
		return false
	}
	if strings.EqualFold(mode, "s") {
		return strings.EqualFold(fromDomain, authDomain)
	}
	return organizationalDomain(fromDomain) == organizationalDomain(authDomain)
}

func lookupDMARC(domain string) (record string, policyDomain string, ok bool, err error) {
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	if domain == "" {
		return "", "", false, nil
	}
	if rec, ok, err := lookupDMARCAt(domain); err != nil || ok {
		return rec, domain, ok, err
	}
	org := organizationalDomain(domain)
	if org != "" && org != domain {
		rec, ok, err := lookupDMARCAt(org)
		return rec, org, ok, err
	}
	return "", "", false, nil
}

func lookupDMARCAt(domain string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	txt, err := dmarcLookupTXT(ctx, "_dmarc."+domain)
	if err != nil {
		return "", false, err
	}
	for _, t := range txt {
		s := strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(s), "v=dmarc1") {
			return s, true, nil
		}
	}
	return "", false, nil
}

func organizationalDomain(domain string) string {
	domain = strings.Trim(strings.ToLower(domain), ".")
	if domain == "" {
		return ""
	}
	labels := strings.Split(domain, ".")
	if len(labels) <= 2 {
		return domain
	}
	publicSuffix2 := labels[len(labels)-2] + "." + labels[len(labels)-1]
	twoLevelPSL := []string{
		"co.uk", "org.uk", "gov.uk", "ac.uk",
		"co.jp", "or.jp", "ne.jp",
		"com.au", "net.au", "org.au",
		"co.nz", "co.kr", "co.in",
		"com.br", "com.mx", "com.tr",
		"com.cn", "com.tw", "com.hk", "com.sg",
	}
	if slices.Contains(twoLevelPSL, publicSuffix2) && len(labels) >= 3 {
		return labels[len(labels)-3] + "." + publicSuffix2
	}
	return publicSuffix2
}

func parseDMARCInt(v string, def int) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func parseDMARCList(v string, def []string) []string {
	return parseDMARCTokenList(v, def, ",")
}

func parseDMARCTokenList(v string, def []string, sep string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		if def == nil {
			return nil
		}
		out := make([]string, len(def))
		copy(out, def)
		return out
	}
	parts := strings.Split(v, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 && def != nil {
		out = make([]string, len(def))
		copy(out, def)
	}
	return out
}
