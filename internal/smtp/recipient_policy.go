package smtp

import (
	"context"
	"net"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

type recipientAuthorizer interface {
	Authorize(context.Context, net.IP, bool, string) (string, error)
}

// An empty reason permits the recipient. Other reasons are fixed internal values.
type staticRecipientPolicy struct {
	domains    map[string]bool
	recipients map[string]bool
	trusted    []*net.IPNet
}

func newRecipientPolicy(cfg config.InboundPolicyConfig) (recipientAuthorizer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p := &staticRecipientPolicy{domains: map[string]bool{}, recipients: map[string]bool{}}
	for _, d := range cfg.LocalDomains {
		p.domains[strings.ToLower(d)] = true
	}
	for _, r := range cfg.LocalRecipients {
		p.recipients[strings.ToLower(r)] = true
	}
	for _, cidr := range cfg.TrustedCIDRs {
		_, network, _ := net.ParseCIDR(cidr) // Validated above.
		p.trusted = append(p.trusted, network)
	}
	return p, nil
}

func (p *staticRecipientPolicy) Authorize(_ context.Context, peer net.IP, authenticated bool, recipient string) (string, error) {
	domain, ok := util.DomainOf(recipient)
	if !ok {
		return "relay_denied", nil
	}
	if p.domains[domain] {
		if p.recipients[strings.ToLower(recipient)] {
			return "", nil
		}
		return "unknown_recipient", nil
	}
	if authenticated {
		return "", nil
	}
	for _, network := range p.trusted {
		if peer != nil && network.Contains(peer) {
			return "", nil
		}
	}
	return "relay_denied", nil
}
