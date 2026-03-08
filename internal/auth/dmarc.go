package auth

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/tamago/orinoco-mta/internal/util"
)

func EvalDMARC(fromDomain string, spf SPFResult, dkim DKIMResult) DMARCResult {
	if fromDomain == "" {
		return DMARCResult{Result: "permerror", Reason: "missing From domain"}
	}
	rec, ok, err := lookupDMARC(fromDomain)
	if err != nil {
		return DMARCResult{Domain: fromDomain, Result: "temperror", Reason: err.Error()}
	}
	if !ok {
		return DMARCResult{Domain: fromDomain, Result: "none", Policy: "none", Reason: "no dmarc record"}
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

	spfAligned := strings.EqualFold(spf.Result, "pass") && aligned(fromDomain, spf.Domain, aspf)
	dkimAligned := false
	for _, s := range dkim.Sigs {
		if strings.EqualFold(s.Result, "pass") && aligned(fromDomain, s.Domain, adkim) {
			dkimAligned = true
			break
		}
	}
	if spfAligned || dkimAligned {
		return DMARCResult{Domain: fromDomain, Result: "pass", Policy: policy}
	}
	return DMARCResult{Domain: fromDomain, Result: "fail", Policy: policy, Reason: "alignment failed"}
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
	if strings.EqualFold(fromDomain, authDomain) {
		return true
	}
	return strings.HasSuffix(fromDomain, "."+authDomain) || strings.HasSuffix(authDomain, "."+fromDomain)
}

func lookupDMARC(domain string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	txt, err := net.DefaultResolver.LookupTXT(ctx, "_dmarc."+domain)
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
