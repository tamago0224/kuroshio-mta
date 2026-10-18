package config

import (
	"errors"
	"net"
)

// ProxyProtocolConfig makes each selected listener exclusively accept PROXY v2.
type ProxyProtocolConfig struct {
	SMTP          bool     `yaml:"smtp"`
	Submission    bool     `yaml:"submission"`
	SubmissionTLS bool     `yaml:"submission_tls"`
	TrustedCIDRs  []string `yaml:"trusted_cidrs"`
}

func (c ProxyProtocolConfig) Validate() error {
	if (c.SMTP || c.Submission || c.SubmissionTLS) && len(c.TrustedCIDRs) == 0 {
		return errors.New("proxy_protocol requires trusted_cidrs when a listener is enabled")
	}
	for _, cidr := range c.TrustedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("proxy_protocol.trusted_cidrs contains an invalid CIDR")
		}
	}
	return nil
}
