package auth

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/tamago/orinoco-mta/internal/util"
)

func EvalSPF(remoteIP net.IP, mailFrom, helo string) SPFResult {
	domain := spfDomain(mailFrom, helo)
	if domain == "" {
		return SPFResult{Result: "none", Reason: "no domain"}
	}
	if remoteIP == nil {
		return SPFResult{Domain: domain, Result: "temperror", Reason: "missing remote ip"}
	}
	res, reason := evalSPFDomain(context.Background(), remoteIP, domain, 0)
	return SPFResult{Domain: domain, Result: res, Reason: reason}
}

func spfDomain(mailFrom, helo string) string {
	if d, ok := util.DomainOf(mailFrom); ok {
		return d
	}
	return strings.ToLower(strings.TrimSpace(helo))
}

func evalSPFDomain(ctx context.Context, remoteIP net.IP, domain string, depth int) (string, string) {
	if depth > 10 {
		return "permerror", "include recursion too deep"
	}
	record, ok, err := lookupSPFRecord(ctx, domain)
	if err != nil {
		return "temperror", err.Error()
	}
	if !ok {
		return "none", "no spf record"
	}
	terms := strings.Fields(record)
	if len(terms) == 0 || strings.ToLower(terms[0]) != "v=spf1" {
		return "permerror", "invalid spf header"
	}
	for _, t := range terms[1:] {
		if t == "" {
			continue
		}
		if strings.Contains(t, "=") {
			continue
		}
		qualifier := '+'
		if strings.ContainsAny(t[:1], "+-~?") {
			qualifier = rune(t[0])
			t = t[1:]
		}
		name, arg, prefix := parseMechanism(t)
		match, ok, err := matchMechanism(ctx, remoteIP, domain, name, arg, prefix, depth)
		if err != nil {
			return "temperror", err.Error()
		}
		if !ok {
			continue
		}
		if match {
			return qualifierResult(qualifier), fmt.Sprintf("matched %s", name)
		}
	}
	return "neutral", "no mechanism matched"
}

func lookupSPFRecord(ctx context.Context, domain string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	txt, err := net.DefaultResolver.LookupTXT(ctx, domain)
	if err != nil {
		return "", false, err
	}
	for _, v := range txt {
		s := strings.TrimSpace(v)
		if strings.HasPrefix(strings.ToLower(s), "v=spf1") {
			return s, true, nil
		}
	}
	return "", false, nil
}

func parseMechanism(term string) (name, arg string, prefix int) {
	prefix = -1
	name = term
	if i := strings.Index(term, ":"); i >= 0 {
		name = term[:i]
		arg = term[i+1:]
	}
	if i := strings.Index(arg, "/"); i >= 0 {
		p := arg[i+1:]
		arg = arg[:i]
		fmt.Sscanf(p, "%d", &prefix)
	}
	name = strings.ToLower(name)
	return
}

func qualifierResult(q rune) string {
	switch q {
	case '-':
		return "fail"
	case '~':
		return "softfail"
	case '?':
		return "neutral"
	default:
		return "pass"
	}
}

func matchMechanism(ctx context.Context, remoteIP net.IP, domain, name, arg string, prefix, depth int) (bool, bool, error) {
	switch name {
	case "all":
		return true, true, nil
	case "ip4", "ip6":
		if arg == "" {
			return false, false, nil
		}
		ok, err := matchIPMechanism(remoteIP, arg)
		return ok, true, err
	case "a":
		host := domain
		if arg != "" {
			host = arg
		}
		ok, err := matchHostIPs(ctx, remoteIP, host, prefix)
		return ok, true, err
	case "mx":
		host := domain
		if arg != "" {
			host = arg
		}
		ok, err := matchMX(ctx, remoteIP, host, prefix)
		return ok, true, err
	case "include":
		if arg == "" {
			return false, true, nil
		}
		res, _ := evalSPFDomain(ctx, remoteIP, arg, depth+1)
		if res == "pass" {
			return true, true, nil
		}
		return false, true, nil
	default:
		return false, false, nil
	}
}

func matchIPMechanism(remoteIP net.IP, cidr string) (bool, error) {
	if !strings.Contains(cidr, "/") {
		if strings.Contains(cidr, ":") {
			cidr += "/128"
		} else {
			cidr += "/32"
		}
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false, err
	}
	addr, ok := netip.AddrFromSlice(remoteIP)
	if !ok {
		return false, fmt.Errorf("invalid remote ip")
	}
	return prefix.Contains(addr.Unmap()), nil
}

func matchHostIPs(ctx context.Context, remoteIP net.IP, host string, prefix int) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, ip := range ips {
		if ip.Equal(remoteIP) {
			return true, nil
		}
		if prefix >= 0 {
			if matchPrefix(remoteIP, ip, prefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

func matchMX(ctx context.Context, remoteIP net.IP, domain string, prefix int) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	mx, err := net.DefaultResolver.LookupMX(ctx, domain)
	if err != nil {
		return false, err
	}
	for _, m := range mx {
		host := strings.TrimSuffix(m.Host, ".")
		ok, err := matchHostIPs(ctx, remoteIP, host, prefix)
		if err != nil {
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func matchPrefix(a, b net.IP, p int) bool {
	aa, ok1 := netip.AddrFromSlice(a)
	bb, ok2 := netip.AddrFromSlice(b)
	if !ok1 || !ok2 {
		return false
	}
	if aa.Is4() != bb.Is4() {
		return false
	}
	pref := netip.PrefixFrom(aa.Unmap(), p)
	return pref.Contains(bb.Unmap())
}
