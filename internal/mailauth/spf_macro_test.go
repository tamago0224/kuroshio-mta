package mailauth

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestEvalSPF_MacroExpansionForAAndInclude(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
	})

	records := map[string]string{
		"example.com":         "v=spf1 a:%{d} include:%{l}.example.net -all",
		"sender.example.net":  "v=spf1 ip4:198.51.100.20 -all",
	}
	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		domain = strings.ToLower(domain)
		if v, ok := records[domain]; ok {
			return []string{v}, nil
		}
		return nil, nil
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if strings.EqualFold(host, "example.com") {
			return []net.IP{net.ParseIP("192.0.2.10")}, nil
		}
		return nil, nil
	}
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) {
		return nil, nil
	}

	res := EvalSPF(net.ParseIP("198.51.100.20"), "sender@example.com", "mx.example.com")
	if res.Result != "pass" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestExpandSPFMacros_BasicTokens(t *testing.T) {
	ip := net.ParseIP("203.0.113.7")
	got := expandSPFMacros("%{s}/%{l}/%{o}/%{d}/%{h}/%{i}/%{v}/%%/%_/%-", "Alice@Example.com", "example.com", "Mail.EXAMPLE.com", ip)
	want := "alice@example.com/alice/example.com/example.com/mail.example.com/203.0.113.7/in-addr/%/ /%20"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}
