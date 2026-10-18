package config

import (
	"fmt"
	"net"
	"net/mail"
	"strings"
)

// InboundPolicyConfig defines recipient admission independently of delivery routing.
type InboundPolicyConfig struct {
	LocalDomains    []string `yaml:"local_domains"`
	LocalRecipients []string `yaml:"local_recipients"`
	TrustedCIDRs    []string `yaml:"trusted_cidrs"`
}

func (c InboundPolicyConfig) Validate() error {
	domains := make(map[string]bool, len(c.LocalDomains))
	for _, raw := range c.LocalDomains {
		domain := strings.ToLower(raw)
		if !validLocalDomain(domain) {
			return fmt.Errorf("inbound_policy.local_domains contains an invalid domain")
		}
		domains[domain] = true
	}
	for _, raw := range c.LocalRecipients {
		addr, err := mail.ParseAddress(raw)
		at := strings.LastIndexByte(raw, '@')
		if err != nil || addr.Address != raw || at <= 0 || strings.ContainsAny(raw, "<> \t\r\n") || !domains[strings.ToLower(raw[at+1:])] {
			return fmt.Errorf("inbound_policy.local_recipients must contain mailboxes in local_domains")
		}
		for _, b := range []byte(raw) {
			if b >= 128 {
				return fmt.Errorf("inbound_policy.local_recipients requires ASCII mailboxes")
			}
		}
	}
	for _, cidr := range c.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("inbound_policy.trusted_cidrs contains an invalid CIDR")
		}
	}
	return nil
}

func validLocalDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
