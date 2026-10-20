package dmarcreport

import (
	"context"
	"errors"
	"net"
	"strings"
)

type TXTLookup func(context.Context, string) ([]string, error)

// AuthorizeExternalDestination applies RFC 9990's external RUA destination
// check. policyOrg and destinationOrg must come from the active policy engine.
func AuthorizeExternalDestination(ctx context.Context, policyDomain, policyOrg, destinationDomain, destinationOrg string, lookup TXTLookup) (bool, error) {
	if policyOrg == "" || destinationOrg == "" || policyDomain == "" || destinationDomain == "" {
		return false, errors.New("report authorization domains are required")
	}
	if strings.EqualFold(policyOrg, destinationOrg) {
		return true, nil
	}
	name := policyDomain + "._report._dmarc." + destinationDomain
	if len(name) > 253 {
		return false, nil
	}
	answers, err := lookup(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, nil
		}
		return false, err
	}
	for _, answer := range answers {
		if validAuthorizationRecord(answer) {
			return true, nil
		}
	}
	return false, nil
}

func validAuthorizationRecord(record string) bool {
	parts := strings.Split(record, ";")
	seen := map[string]struct{}{}
	first := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return false
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return false
		}
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		if first == "" {
			if key != "v" {
				return false
			}
			first = strings.ToLower(value)
		}
	}
	return first == "dmarc1"
}
