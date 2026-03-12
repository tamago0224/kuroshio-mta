package mailauth

import (
	"context"
	"errors"
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

func TestEvalSPF_ExistsMechanism(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		if strings.EqualFold(domain, "example.com") {
			return []string{"v=spf1 exists:%{l}.exists.example.net -all"}, nil
		}
		return nil, nil
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if strings.EqualFold(host, "sender.exists.example.net") {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		}
		return nil, nil
	}
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("192.0.2.1"), "sender@example.com", "mx.example.com")
	if res.Result != "pass" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestEvalSPF_PTRMechanism(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		if strings.EqualFold(domain, "example.com") {
			return []string{"v=spf1 ptr:trusted.example.net -all"}, nil
		}
		return nil, nil
	}
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) {
		if addr == "198.51.100.10" {
			return []string{"mx.trusted.example.net."}, nil
		}
		return nil, nil
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if strings.EqualFold(host, "mx.trusted.example.net") {
			return []net.IP{net.ParseIP("198.51.100.10")}, nil
		}
		return nil, nil
	}
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("198.51.100.10"), "sender@example.com", "mx.example.com")
	if res.Result != "pass" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestEvalSPF_RedirectModifier(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 redirect=spf.redirect.example.net"}, nil
		case "spf.redirect.example.net":
			return []string{"v=spf1 ip4:203.0.113.9 -all"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.9"), "sender@example.com", "mx.example.com")
	if res.Result != "pass" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestEvalSPF_ExpModifierForFail(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 -all exp=explain.example.com"}, nil
		case "explain.example.com":
			return []string{"Mail from this source is not permitted."}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.77"), "sender@example.com", "mx.example.com")
	if res.Result != "fail" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
	if !strings.Contains(res.Reason, "not permitted") {
		t.Fatalf("unexpected exp reason=%q", res.Reason)
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

func TestEvalSPF_DNSLookupLimitExceeded(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		if strings.EqualFold(domain, "example.com") {
			return []string{"v=spf1 a:a1.example.net a:a2.example.net a:a3.example.net a:a4.example.net a:a5.example.net a:a6.example.net a:a7.example.net a:a8.example.net a:a9.example.net a:a10.example.net a:a11.example.net -all"}, nil
		}
		return nil, nil
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.1")}, nil
	}
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("198.51.100.10"), "sender@example.com", "mx.example.com")
	if res.Result != "permerror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
	if !strings.Contains(strings.ToLower(res.Reason), "lookup limit") {
		t.Fatalf("unexpected reason=%q", res.Reason)
	}
}

func TestEvalSPF_VoidLookupLimitExceeded(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		if strings.EqualFold(domain, "example.com") {
			return []string{"v=spf1 exists:v1.example.net exists:v2.example.net exists:v3.example.net -all"}, nil
		}
		return nil, nil
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		return nil, nil
	}
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("198.51.100.10"), "sender@example.com", "mx.example.com")
	if res.Result != "permerror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
	if !strings.Contains(strings.ToLower(res.Reason), "void lookup") {
		t.Fatalf("unexpected reason=%q", res.Reason)
	}
}

func TestEvalSPF_IncludePermerrorPropagates(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 include:bad.example.net -all"}, nil
		case "bad.example.net":
			return []string{"v=spf1x -all"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.10"), "sender@example.com", "mx.example.com")
	if res.Result != "permerror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestEvalSPF_IncludeTemperrorPropagates(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 include:tmp.example.net -all"}, nil
		case "tmp.example.net":
			return nil, errors.New("dns timeout")
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.10"), "sender@example.com", "mx.example.com")
	if res.Result != "temperror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
}

func TestEvalSPF_CyclicIncludeReturnsPermerror(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 include:a.example.net -all"}, nil
		case "a.example.net":
			return []string{"v=spf1 include:b.example.net -all"}, nil
		case "b.example.net":
			return []string{"v=spf1 include:a.example.net -all"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.10"), "sender@example.com", "mx.example.com")
	if res.Result != "permerror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
	if !strings.Contains(strings.ToLower(res.Reason), "cyclic") {
		t.Fatalf("unexpected reason=%q", res.Reason)
	}
}

func TestEvalSPF_CyclicRedirectReturnsPermerror(t *testing.T) {
	origTXT := spfLookupTXT
	origIP := spfLookupIP
	origMX := spfLookupMX
	origAddr := spfLookupAddr
	t.Cleanup(func() {
		spfLookupTXT = origTXT
		spfLookupIP = origIP
		spfLookupMX = origMX
		spfLookupAddr = origAddr
	})

	spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
		switch strings.ToLower(domain) {
		case "example.com":
			return []string{"v=spf1 redirect=a.example.net"}, nil
		case "a.example.net":
			return []string{"v=spf1 redirect=b.example.net"}, nil
		case "b.example.net":
			return []string{"v=spf1 redirect=a.example.net"}, nil
		default:
			return nil, nil
		}
	}
	spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) { return nil, nil }
	spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) { return nil, nil }
	spfLookupAddr = func(_ context.Context, addr string) ([]string, error) { return nil, nil }

	res := EvalSPF(net.ParseIP("203.0.113.10"), "sender@example.com", "mx.example.com")
	if res.Result != "permerror" {
		t.Fatalf("result=%q reason=%q", res.Result, res.Reason)
	}
	if !strings.Contains(strings.ToLower(res.Reason), "cyclic") {
		t.Fatalf("unexpected reason=%q", res.Reason)
	}
}
