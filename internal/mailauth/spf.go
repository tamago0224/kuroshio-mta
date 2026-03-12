package mailauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/util"
)

const (
	spfMaxDNSLookups  = 10
	spfMaxVoidLookups = 2
)

var (
	errSPFLookupLimit     = errors.New("spf dns lookup limit exceeded")
	errSPFVoidLookupLimit = errors.New("spf void lookup limit exceeded")
	errSPFCyclicReference = errors.New("spf cyclic include/redirect detected")
)

var (
	spfLookupTXT = func(ctx context.Context, domain string) ([]string, error) {
		return net.DefaultResolver.LookupTXT(ctx, domain)
	}
	spfLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}
	spfLookupMX = func(ctx context.Context, domain string) ([]*net.MX, error) {
		return net.DefaultResolver.LookupMX(ctx, domain)
	}
	spfLookupAddr = func(ctx context.Context, addr string) ([]string, error) {
		return net.DefaultResolver.LookupAddr(ctx, addr)
	}
)

func EvalSPF(remoteIP net.IP, mailFrom, helo string) SPFResult {
	domain := spfDomain(mailFrom, helo)
	if domain == "" {
		return SPFResult{Result: "none", Reason: "no domain"}
	}
	if remoteIP == nil {
		return SPFResult{Domain: domain, Result: "temperror", Reason: "missing remote ip"}
	}
	state := &spfEvalState{}
	res, reason := evalSPFDomain(context.Background(), remoteIP, domain, mailFrom, helo, 0, state)
	return SPFResult{Domain: domain, Result: res, Reason: reason}
}

func spfDomain(mailFrom, helo string) string {
	if d, ok := util.DomainOf(mailFrom); ok {
		return d
	}
	return strings.ToLower(strings.TrimSpace(helo))
}

func evalSPFDomain(ctx context.Context, remoteIP net.IP, domain, sender, helo string, depth int, st *spfEvalState) (string, string) {
	if depth > 10 {
		return "permerror", "include recursion too deep"
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "permerror", "invalid domain"
	}
	if !st.enterDomain(domain) {
		return "permerror", errSPFCyclicReference.Error()
	}
	defer st.leaveDomain(domain)

	record, ok, err := lookupSPFRecord(ctx, domain, st)
	if err != nil {
		var rerr *spfResultError
		if errors.As(err, &rerr) {
			return rerr.Result, rerr.Reason
		}
		if errors.Is(err, errSPFLookupLimit) || errors.Is(err, errSPFVoidLookupLimit) {
			return "permerror", err.Error()
		}
		return "temperror", err.Error()
	}
	if !ok {
		return "none", "no spf record"
	}
	terms := strings.Fields(record)
	if len(terms) == 0 || strings.ToLower(terms[0]) != "v=spf1" {
		return "permerror", "invalid spf header"
	}
	redirectDomain := ""
	explanationDomain := ""
	for _, t := range terms[1:] {
		if t == "" || !strings.Contains(t, "=") {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "redirect":
			redirectDomain = expandSPFMacros(strings.TrimSpace(v), sender, domain, helo, remoteIP)
		case "exp":
			explanationDomain = expandSPFMacros(strings.TrimSpace(v), sender, domain, helo, remoteIP)
		}
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
		arg = expandSPFMacros(arg, sender, domain, helo, remoteIP)
		match, ok, err := matchMechanism(ctx, remoteIP, domain, sender, helo, name, arg, prefix, depth, st)
		if err != nil {
			var rerr *spfResultError
			if errors.As(err, &rerr) {
				return rerr.Result, rerr.Reason
			}
			if errors.Is(err, errSPFLookupLimit) || errors.Is(err, errSPFVoidLookupLimit) {
				return "permerror", err.Error()
			}
			return "temperror", err.Error()
		}
		if !ok {
			continue
		}
		if match {
			result := qualifierResult(qualifier)
			if result == "fail" && explanationDomain != "" {
				if exp, ok := lookupSPFExplanation(ctx, explanationDomain); ok {
					return result, exp
				}
			}
			return result, fmt.Sprintf("matched %s", name)
		}
	}
	if redirectDomain != "" {
		res, reason := evalSPFDomain(ctx, remoteIP, redirectDomain, sender, helo, depth+1, st)
		if res == "none" {
			return "permerror", "redirect target has no spf record"
		}
		return res, reason
	}
	return "neutral", "no mechanism matched"
}

func lookupSPFRecord(ctx context.Context, domain string, st *spfEvalState) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	txt, err := lookupTXT(ctx, domain, st)
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

func matchMechanism(ctx context.Context, remoteIP net.IP, domain, sender, helo, name, arg string, prefix, depth int, st *spfEvalState) (bool, bool, error) {
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
		ok, err := matchHostIPs(ctx, remoteIP, host, prefix, st)
		return ok, true, err
	case "mx":
		host := domain
		if arg != "" {
			host = arg
		}
		ok, err := matchMX(ctx, remoteIP, host, prefix, st)
		return ok, true, err
	case "include":
		if arg == "" {
			return false, true, nil
		}
		res, reason := evalSPFDomain(ctx, remoteIP, arg, sender, helo, depth+1, st)
		if res == "pass" {
			return true, true, nil
		}
		if res == "permerror" || res == "temperror" {
			return false, true, &spfResultError{Result: res, Reason: reason}
		}
		return false, true, nil
	case "exists":
		if arg == "" {
			return false, true, nil
		}
		ok, err := matchExists(ctx, arg, st)
		return ok, true, err
	case "ptr":
		host := domain
		if arg != "" {
			host = arg
		}
		ok, err := matchPTR(ctx, remoteIP, host, st)
		return ok, true, err
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

func matchHostIPs(ctx context.Context, remoteIP net.IP, host string, prefix int, st *spfEvalState) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ips, err := lookupIP(ctx, host, st)
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

func matchMX(ctx context.Context, remoteIP net.IP, domain string, prefix int, st *spfEvalState) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	mx, err := lookupMX(ctx, domain, st)
	if err != nil {
		return false, err
	}
	for _, m := range mx {
		host := strings.TrimSuffix(m.Host, ".")
		ok, err := matchHostIPs(ctx, remoteIP, host, prefix, st)
		if err != nil {
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func matchExists(ctx context.Context, host string, st *spfEvalState) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ips, err := lookupIP(ctx, host, st)
	if err != nil {
		return false, err
	}
	return len(ips) > 0, nil
}

func matchPTR(ctx context.Context, remoteIP net.IP, targetDomain string, st *spfEvalState) (bool, error) {
	if remoteIP == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	names, err := lookupAddr(ctx, remoteIP.String(), st)
	if err != nil {
		return false, err
	}
	targetDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(targetDomain), "."))
	for _, n := range names {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if !domainMatches(host, targetDomain) {
			continue
		}
		ips, err := lookupIP(ctx, host, st)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if ip.Equal(remoteIP) {
				return true, nil
			}
		}
	}
	return false, nil
}

func domainMatches(host, domain string) bool {
	if host == "" || domain == "" {
		return false
	}
	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain)
}

func expandSPFMacros(spec, sender, domain, helo string, remoteIP net.IP) string {
	if spec == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(spec); i++ {
		ch := spec[i]
		if ch != '%' {
			b.WriteByte(ch)
			continue
		}
		if i+1 >= len(spec) {
			break
		}
		next := spec[i+1]
		i++
		switch next {
		case '%':
			b.WriteByte('%')
		case '_':
			b.WriteByte(' ')
		case '-':
			b.WriteString("%20")
		case '{':
			end := strings.IndexByte(spec[i+1:], '}')
			if end < 0 {
				continue
			}
			token := spec[i+1 : i+1+end]
			i += end + 1
			b.WriteString(spfMacroValue(token, sender, domain, helo, remoteIP))
		default:
			b.WriteByte(next)
		}
	}
	return b.String()
}

func spfMacroValue(token, sender, domain, helo string, remoteIP net.IP) string {
	if token == "" {
		return ""
	}
	letter := token[0]
	switch letter {
	case 's', 'S':
		return strings.ToLower(strings.TrimSpace(sender))
	case 'l', 'L':
		local, _, ok := strings.Cut(strings.TrimSpace(sender), "@")
		if !ok {
			return ""
		}
		return strings.ToLower(local)
	case 'o', 'O':
		_, d, ok := strings.Cut(strings.TrimSpace(sender), "@")
		if !ok {
			return ""
		}
		return strings.ToLower(d)
	case 'd', 'D':
		return strings.ToLower(strings.TrimSpace(domain))
	case 'h', 'H':
		return strings.ToLower(strings.TrimSpace(helo))
	case 'i', 'I':
		if remoteIP == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(remoteIP.String()))
	case 'v', 'V':
		if remoteIP == nil {
			return ""
		}
		if remoteIP.To4() != nil {
			return "in-addr"
		}
		return "ip6"
	case 'p', 'P':
		return "unknown"
	case 'c', 'C':
		if remoteIP == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(remoteIP.String()))
	case 'r', 'R':
		return "unknown"
	case 't', 'T':
		return strconv.FormatInt(time.Now().UTC().Unix(), 10)
	default:
		return ""
	}
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

func lookupSPFExplanation(ctx context.Context, domain string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	txt, err := spfLookupTXT(ctx, domain)
	if err != nil || len(txt) == 0 {
		return "", false
	}
	for _, v := range txt {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(s), "v=spf1") {
			continue
		}
		return s, true
	}
	return "", false
}

type spfEvalState struct {
	lookups     int
	voidLookups int
	active      map[string]struct{}
}

type spfResultError struct {
	Result string
	Reason string
}

func (e *spfResultError) Error() string {
	return e.Result + ": " + e.Reason
}

func (s *spfEvalState) enterDomain(domain string) bool {
	if s == nil {
		return true
	}
	if s.active == nil {
		s.active = map[string]struct{}{}
	}
	if _, exists := s.active[domain]; exists {
		return false
	}
	s.active[domain] = struct{}{}
	return true
}

func (s *spfEvalState) leaveDomain(domain string) {
	if s == nil || s.active == nil {
		return
	}
	delete(s.active, domain)
}

func (s *spfEvalState) consumeLookup() error {
	if s == nil {
		return nil
	}
	s.lookups++
	if s.lookups > spfMaxDNSLookups {
		return errSPFLookupLimit
	}
	return nil
}

func (s *spfEvalState) consumeVoid() error {
	if s == nil {
		return nil
	}
	s.voidLookups++
	if s.voidLookups > spfMaxVoidLookups {
		return errSPFVoidLookupLimit
	}
	return nil
}

func lookupTXT(ctx context.Context, domain string, st *spfEvalState) ([]string, error) {
	if err := st.consumeLookup(); err != nil {
		return nil, err
	}
	txt, err := spfLookupTXT(ctx, domain)
	if err != nil {
		return nil, err
	}
	if len(txt) == 0 {
		if err := st.consumeVoid(); err != nil {
			return nil, err
		}
	}
	return txt, nil
}

func lookupIP(ctx context.Context, host string, st *spfEvalState) ([]net.IP, error) {
	if err := st.consumeLookup(); err != nil {
		return nil, err
	}
	ips, err := spfLookupIP(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		if err := st.consumeVoid(); err != nil {
			return nil, err
		}
	}
	return ips, nil
}

func lookupMX(ctx context.Context, domain string, st *spfEvalState) ([]*net.MX, error) {
	if err := st.consumeLookup(); err != nil {
		return nil, err
	}
	mx, err := spfLookupMX(ctx, domain)
	if err != nil {
		return nil, err
	}
	if len(mx) == 0 {
		if err := st.consumeVoid(); err != nil {
			return nil, err
		}
	}
	return mx, nil
}

func lookupAddr(ctx context.Context, addr string, st *spfEvalState) ([]string, error) {
	if err := st.consumeLookup(); err != nil {
		return nil, err
	}
	names, err := spfLookupAddr(ctx, addr)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		if err := st.consumeVoid(); err != nil {
			return nil, err
		}
	}
	return names, nil
}
