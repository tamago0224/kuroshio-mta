package mailauth

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

type spfRFCVector struct {
	name      string
	remoteIP  string
	mailFrom  string
	helo      string
	txt       map[string][]string
	ip        map[string][]net.IP
	mx        map[string][]*net.MX
	ptr       map[string][]string
	txtErr    map[string]error
	expectRes string
}

func TestEvalSPF_RFCStyleVectors(t *testing.T) {
	vectors := []spfRFCVector{
		{
			name:      "pass with ip4 mechanism",
			remoteIP:  "192.0.2.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1 ip4:192.0.2.10 -all"}},
			expectRes: "pass",
		},
		{
			name:      "fail with -all",
			remoteIP:  "198.51.100.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1 -all"}},
			expectRes: "fail",
		},
		{
			name:      "softfail with ~all",
			remoteIP:  "198.51.100.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1 ~all"}},
			expectRes: "softfail",
		},
		{
			name:      "neutral with ?all",
			remoteIP:  "198.51.100.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1 ?all"}},
			expectRes: "neutral",
		},
		{
			name:     "pass with include",
			remoteIP: "203.0.113.7",
			mailFrom: "sender@example.com",
			helo:     "mx.example.com",
			txt: map[string][]string{
				"example.com":     {"v=spf1 include:spf.example.net -all"},
				"spf.example.net": {"v=spf1 ip4:203.0.113.7 -all"},
			},
			expectRes: "pass",
		},
		{
			name:     "pass with mx mechanism",
			remoteIP: "203.0.113.25",
			mailFrom: "sender@example.com",
			helo:     "mx.example.com",
			txt:      map[string][]string{"example.com": {"v=spf1 mx -all"}},
			mx: map[string][]*net.MX{
				"example.com": {{Host: "mail.example.com.", Pref: 10}},
			},
			ip: map[string][]net.IP{
				"mail.example.com": {net.ParseIP("203.0.113.25")},
			},
			expectRes: "pass",
		},
		{
			name:      "permerror with invalid spf header",
			remoteIP:  "203.0.113.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1x -all"}},
			expectRes: "permerror",
		},
		{
			name:      "permerror with multiple spf records",
			remoteIP:  "203.0.113.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txt:       map[string][]string{"example.com": {"v=spf1 -all", "v=spf1 ip4:203.0.113.10 -all"}},
			expectRes: "permerror",
		},
		{
			name:      "temperror with dns timeout",
			remoteIP:  "203.0.113.10",
			mailFrom:  "sender@example.com",
			helo:      "mx.example.com",
			txtErr:    map[string]error{"example.com": errors.New("dns timeout")},
			expectRes: "temperror",
		},
	}

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

	for _, tc := range vectors {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			spfLookupTXT = func(_ context.Context, domain string) ([]string, error) {
				d := strings.ToLower(strings.TrimSpace(domain))
				if tc.txtErr != nil {
					if err, ok := tc.txtErr[d]; ok {
						return nil, err
					}
				}
				if tc.txt != nil {
					if v, ok := tc.txt[d]; ok {
						return v, nil
					}
				}
				return nil, nil
			}
			spfLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
				h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
				if tc.ip != nil {
					if v, ok := tc.ip[h]; ok {
						return v, nil
					}
				}
				return nil, nil
			}
			spfLookupMX = func(_ context.Context, domain string) ([]*net.MX, error) {
				d := strings.ToLower(strings.TrimSpace(domain))
				if tc.mx != nil {
					if v, ok := tc.mx[d]; ok {
						return v, nil
					}
				}
				return nil, nil
			}
			spfLookupAddr = func(_ context.Context, addr string) ([]string, error) {
				if tc.ptr != nil {
					if v, ok := tc.ptr[addr]; ok {
						return v, nil
					}
				}
				return nil, nil
			}

			got := EvalSPF(net.ParseIP(tc.remoteIP), tc.mailFrom, tc.helo)
			if got.Result != tc.expectRes {
				t.Fatalf("result=%q want=%q reason=%q", got.Result, tc.expectRes, got.Reason)
			}
		})
	}
}
