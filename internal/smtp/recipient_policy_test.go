package smtp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

// Protocol fixtures opt into permissive admission explicitly; production defaults deny relay.
type allowTestRecipients struct{}

func (allowTestRecipients) Authorize(context.Context, net.IP, bool, string) (string, error) {
	return "", nil
}

type unavailableTestRecipients struct{}

func (unavailableTestRecipients) Authorize(context.Context, net.IP, bool, string) (string, error) {
	return "", errors.New("backend unavailable")
}

func TestRecipientPolicy(t *testing.T) {
	p, err := newRecipientPolicy(config.InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"user@local.test", "postmaster@local.test"}, TrustedCIDRs: []string{"192.0.2.0/24", "2001:db8:1::/48"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, ip, recipient string
		auth                bool
		want                string
	}{
		{name: "external relay denied", ip: "198.51.100.1", recipient: "user@external.test", want: "relay_denied"},
		{name: "unknown peer denied", recipient: "user@external.test", want: "relay_denied"},
		{name: "loopback not implicitly trusted", ip: "127.0.0.1", recipient: "user@external.test", want: "relay_denied"},
		{name: "local mailbox", ip: "198.51.100.1", recipient: "USER@LOCAL.TEST"},
		{name: "unknown local", ip: "198.51.100.1", recipient: "unknown@local.test", want: "unknown_recipient"},
		{name: "authenticated submission", recipient: "user@external.test", auth: true},
		{name: "trusted IPv4", ip: "192.0.2.1", recipient: "user@external.test"},
		{name: "mapped IPv4", ip: "::ffff:192.0.2.1", recipient: "user@external.test"},
		{name: "trusted IPv6", ip: "2001:db8:1::1", recipient: "user@external.test"},
		{name: "untrusted IPv6", ip: "2001:db8:2::1", recipient: "user@external.test", want: "relay_denied"},
		{name: "suffix confusion", ip: "198.51.100.1", recipient: "user@evil-local.test", want: "relay_denied"},
		{name: "trusted unknown local", ip: "192.0.2.1", recipient: "unknown@local.test", want: "unknown_recipient"},
		{name: "authenticated unknown local", recipient: "unknown@local.test", auth: true, want: "unknown_recipient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason, err := p.Authorize(t.Context(), net.ParseIP(tt.ip), tt.auth, tt.recipient)
			if err != nil || reason != tt.want {
				t.Fatalf("reason=%q err=%v want=%q", reason, err, tt.want)
			}
		})
	}
}

func TestRecipientAdmissionSession(t *testing.T) {
	tests := []struct {
		name   string
		cfg    config.InboundPolicyConfig
		policy recipientAuthorizer
		rcpt   string
		code   int
	}{
		{name: "empty policy denies external", rcpt: "user@external.test", code: 550},
		{name: "known local", cfg: config.InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"user@local.test"}}, rcpt: "user@local.test", code: 250},
		{name: "unknown local", cfg: config.InboundPolicyConfig{LocalDomains: []string{"local.test"}}, rcpt: "user@local.test", code: 550},
		{name: "backend unavailable", policy: unavailableTestRecipients{}, rcpt: "user@local.test", code: 451},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := &countingPolicyHook{}
			s := NewServer(config.Config{Hostname: "local.test", InboundPolicy: tt.cfg}, nil, nil)
			s.policyHook = hook
			if tt.policy != nil {
				s.recipientPolicy = tt.policy
			}
			r, w, cleanup := openTestSession(t, s)
			defer cleanup()
			readSMTPResponse(t, r)
			for _, cmd := range []string{"EHLO external.test", "MAIL FROM:<sender@external.test>"} {
				mustWriteSMTPLine(t, w, cmd)
				readSMTPResponse(t, r)
			}
			mustWriteSMTPLine(t, w, "RCPT TO:<"+tt.rcpt+">")
			if _, code := readSMTPResponse(t, r); code != tt.code {
				t.Fatalf("code=%d want=%d", code, tt.code)
			}
			if tt.code != 250 {
				if hook.calls.Load() != 0 {
					t.Fatal("hook must not override recipient denial")
				}
				mustWriteSMTPLine(t, w, "DATA")
				if _, code := readSMTPResponse(t, r); code != 503 {
					t.Fatal("rejected RCPT added to envelope")
				}
			}
		})
	}
}

func TestRecipientPolicyIgnoresUntrustedProxyCommand(t *testing.T) {
	s := NewServer(config.Config{Hostname: "local.test", InboundPolicy: config.InboundPolicyConfig{TrustedCIDRs: []string{"192.0.2.0/24"}}}, nil, nil)
	r, w, cleanup := openTestSession(t, s)
	defer cleanup()
	readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "PROXY TCP4 192.0.2.1 192.0.2.2 1234 25")
	if _, code := readSMTPResponse(t, r); code != 502 && code != 500 {
		t.Fatalf("PROXY code=%d", code)
	}
	for _, cmd := range []string{"EHLO trusted.example", "MAIL FROM:<sender@local.test>"} {
		mustWriteSMTPLine(t, w, cmd)
		readSMTPResponse(t, r)
	}
	mustWriteSMTPLine(t, w, "RCPT TO:<user@external.test>")
	resp, code := readSMTPResponse(t, r)
	if code != 550 || !strings.Contains(resp, "relaying denied") {
		t.Fatalf("spoofed proxy authorized: %s", resp)
	}
}

func TestTrustedRecipientAdmissionUsesTCPPeer(t *testing.T) {
	s := NewServer(config.Config{Hostname: "local.test", DeliveryMode: "relay", InboundPolicy: config.InboundPolicyConfig{TrustedCIDRs: []string{"192.0.2.0/24"}}}, nil, nil)
	client, server := net.Pipe()
	defer client.Close()
	go s.handleConn(envelopePeerConn{Conn: server})
	r, w := bufio.NewReader(client), bufio.NewWriter(client)
	readSMTPResponse(t, r)
	for _, cmd := range []string{"EHLO unrelated.test", "MAIL FROM:<sender@unrelated.test>", "RCPT TO:<user@external.test>"} {
		mustWriteSMTPLine(t, w, cmd)
		if _, code := readSMTPResponse(t, r); code != 250 {
			t.Fatalf("%s code=%d", cmd, code)
		}
	}
}

func TestFailedAuthenticationDoesNotGrantRelay(t *testing.T) {
	backend, err := userauth.NewStatic("alice@local.test:correct")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSubmissionServer(config.Config{Hostname: "local.test", SubmissionAllowInsecureAuth: true}, nil, nil, backend)
	r, w, cleanup := openTestSession(t, s)
	defer cleanup()
	readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "EHLO client")
	readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "AUTH PLAIN AGFsaWNlQGxvY2FsLnRlc3QAd3Jvbmc=")
	if _, code := readSMTPResponse(t, r); code != 535 {
		t.Fatalf("failed AUTH code=%d", code)
	}
	mustWriteSMTPLine(t, w, "MAIL FROM:<alice@local.test>")
	readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "RCPT TO:<user@external.test>")
	if _, code := readSMTPResponse(t, r); code != 550 {
		t.Fatalf("failed AUTH granted relay: %d", code)
	}
}
