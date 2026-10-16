package mailauth

import (
	"context"
	"net"
	"strings"
	"testing"
)

// RFC 7208 Section 7.4 examples.
func TestSPFMacroTransformers(t *testing.T) {
	tests := []struct{ spec, want string }{
		{spec: "%{d2}", want: "example.com"},
		{spec: "%{d2r}", want: "example.email"},
		{spec: "%{d127}", want: "email.example.com"},
		{spec: "%{d999999999999999999999999999}", want: "email.example.com"},
		{spec: "%{l-}", want: "strong.bad"},
		{spec: "%{lr-}", want: "bad.strong"},
		{spec: "%{l1r-}", want: "strong"},
		{spec: "%{ir}.%{v}._spf.%{d2}", want: "3.2.0.192.in-addr._spf.example.com"},
		{spec: "%{S}", want: "strong-bad%40email.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			ctx := spfMacroContext{sender: "strong-bad@email.example.com", domain: "email.example.com", helo: "mx.example.com", remoteIP: net.ParseIP("192.0.2.3")}
			got, err := ctx.expand(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestSPFMacroInvalidSyntax(t *testing.T) {
	ctx := spfMacroContext{sender: "sender@example.com", domain: "example.com", remoteIP: net.ParseIP("192.0.2.3")}
	for _, spec := range []string{"%", "%x", "%{", "%{}", "%{z}", "%{d0}", "%{d00}", "%{d2x}", "%{dr2}", "%{d:}", "%{c}", "%{r}", "%{t}"} {
		t.Run(spec, func(t *testing.T) {
			if value, err := ctx.expand(spec); err == nil {
				t.Fatalf("invalid macro expanded to %q", value)
			}
		})
	}
}

func TestSPFMacroDelimitersAndEscaping(t *testing.T) {
	ctx := spfMacroContext{sender: "A..B-C+D/E_F=G,H@example.com", domain: "example.com", remoteIP: net.ParseIP("2001:db8::cb01")}
	tests := []struct{ spec, want string }{
		{spec: "%{l.-+,/_=}", want: "A..B.C.D.E.F.G.H"},
		{spec: "%{l-}", want: "A..B.C+D/E_F=G,H"},
		{spec: "%{l2r.}", want: ".A"},
		{spec: "%{L}", want: "A..B-C%2BD%2FE_F%3DG%2CH"},
		{spec: "%{i}", want: "2.0.0.1.0.d.b.8.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.c.b.0.1"},
		{spec: "%{ir}", want: "1.0.b.c.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ctx.expand(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("want %q, got %q", tt.want, got)
			}
		})
	}
	ctx.sender = ""
	ctx.helo = "mx.example.com"
	if got, err := ctx.expand("%{s}"); err != nil || got != "postmaster@mx.example.com" {
		t.Fatalf("HELO sender: %q %v", got, err)
	}
	ctx.explanation = true
	if got, err := ctx.expand("%{c} %{r}"); err != nil || got != "2001:db8::cb01 unknown" {
		t.Fatalf("explanation-only macros: %q %v", got, err)
	}
}

func TestSPFMacroDomainLimits(t *testing.T) {
	ctx := spfMacroContext{domain: "example.com"}
	if got, err := ctx.domainSpec("example.com."); err != nil || got != "example.com" {
		t.Fatalf("trailing root: %q %v", got, err)
	}
	long := strings.Repeat("a.", 130) + "example.com"
	got, err := ctx.domainSpec(long)
	if err != nil || len(got) > 253 || !strings.HasSuffix(got, "example.com") {
		t.Fatalf("domain truncation: %q %v", got, err)
	}
	for _, spec := range []string{strings.Repeat("x", 64) + ".example.com", "example..com", "%_.example.com", ""} {
		if _, err := ctx.domainSpec(spec); err == nil {
			t.Fatalf("invalid domain accepted: %q", spec)
		}
	}
}

func TestEvalSPFMacroDelimitersAndExplanation(t *testing.T) {
	oldTXT, oldIP := spfLookupTXT, spfLookupIP
	t.Cleanup(func() { spfLookupTXT, spfLookupIP = oldTXT, oldIP })
	ip := net.ParseIP("192.0.2.3")
	tests := []struct{ name, record, sender, host, want, reason string }{
		{name: "literal equals in mechanism", record: "v=spf1 exists:A=B.example.net -all", sender: "a@example.com", host: "A=B.example.net", want: "pass"},
		{name: "equals delimiter", record: "v=spf1 exists:%{l=}.example.net -all", sender: "A=B@example.com", host: "A.B.example.net", want: "pass"},
		{name: "slash delimiter and CIDR", record: "v=spf1 a:%{l/}.example.net/24 -all", sender: "A/B@example.com", host: "A.B.example.net", want: "pass"},
		{name: "include", record: "v=spf1 include:%{d2r}.example.net -all", sender: "a@example.com", host: "com.example.example.net", want: "pass"},
		{name: "redirect", record: "v=spf1 redirect=%{d2r}.example.net", sender: "a@example.com", host: "com.example.example.net", want: "pass"},
		{name: "bad domain macro", record: "v=spf1 exists:%{c}.example.net -all", sender: "a@example.com", want: "permerror"},
		{name: "bad transformer", record: "v=spf1 exists:%{d0}.example.net -all", sender: "a@example.com", want: "permerror"},
		{name: "explanation", record: "v=spf1 -all exp=explain.%{d2}", sender: "a@example.com", host: "explain.example.com", want: "fail", reason: "IP 192.0.2.3: a%40example.com"},
		{name: "broken explanation keeps fail", record: "v=spf1 -all exp=explain.%{d2}", sender: "a@example.com", host: "explain.example.com", want: "fail", reason: "matched all"},
		{name: "multiple explanations keep fail", record: "v=spf1 -all exp=explain.%{d2}", sender: "a@example.com", host: "explain.example.com", want: "fail", reason: "matched all"},
		{name: "long explanation keeps fail", record: "v=spf1 -all exp=explain.%{d2}", sender: "a@example.com", host: "explain.example.com", want: "fail", reason: "matched all"},
		{name: "control explanation keeps fail", record: "v=spf1 -all exp=explain.%{d2}", sender: "a@example.com", host: "explain.example.com", want: "fail", reason: "matched all"},
		{name: "bad exp domain keeps fail", record: "v=spf1 -all exp=%{c}.example.com", sender: "a@example.com", want: "fail", reason: "matched all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
				if domain == "example.com" {
					return []string{tt.record}, nil
				}
				if domain != tt.host {
					t.Fatalf("unexpected TXT lookup: %s", domain)
				}
				switch tt.name {
				case "explanation":
					return []string{"IP %{c}: %{S}"}, nil
				case "broken explanation keeps fail":
					return []string{"bad %{z}"}, nil
				case "multiple explanations keep fail":
					return []string{"one", "two"}, nil
				case "long explanation keeps fail":
					return []string{strings.Repeat("x", 513)}, nil
				case "control explanation keeps fail":
					return []string{"bad\r\nresponse"}, nil
				default:
					return []string{"v=spf1 ip4:192.0.2.0/24 -all"}, nil
				}
			}
			spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
				if host != tt.host {
					t.Fatalf("unexpected host: %s", host)
				}
				return []net.IP{net.ParseIP("192.0.2.9")}, nil
			}
			got := EvalSPF(ip, tt.sender, "mx.example.com")
			if got.Result != tt.want {
				t.Fatalf("want %s, got %+v", tt.want, got)
			}
			if tt.reason != "" && got.Reason != tt.reason {
				t.Fatalf("reason: %q", got.Reason)
			}
		})
	}
}
