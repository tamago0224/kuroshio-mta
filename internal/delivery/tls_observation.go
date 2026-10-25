package delivery

import (
	"strings"
	"time"
)

// TLSObservation is domain-level transport metadata for RFC 8460 aggregation.
type TLSObservation struct {
	ObservedAt   time.Time
	PolicyDomain string
	PolicyType   string
	PolicyString []string
	MXHost       string
	SendingMTAIP string
	ReceivingIP  string
	Success      bool
	ResultType   string
	Reason       string
}

type tlsAttemptMetadata struct{ sendingIP, receivingIP string }
type tlsAttemptMetadataKey struct{}

// SetTLSObserver installs a non-blocking observer owned by the caller.
func (c *Client) SetTLSObserver(observer func(TLSObservation)) { c.tlsObserver = observer }

func tlsResultType(err error) string {
	if err == nil {
		return ""
	}
	classification := ClassifyFailure(err)
	if classification.Stage == StageDNS {
		return "dns-error"
	}
	if classification.Stage != StageTLS {
		return "connection-failure"
	}
	line := strings.ToLower(err.Error())
	switch {
	case strings.Contains(line, "requires starttls"):
		return "starttls-not-supported"
	case strings.Contains(line, "expired"):
		return "certificate-expired"
	case strings.Contains(line, "hostname"), strings.Contains(line, "host mismatch"):
		return "certificate-host-mismatch"
	case strings.Contains(line, "unknown authority"), strings.Contains(line, "not trusted"):
		return "certificate-not-trusted"
	default:
		return "validation-failure"
	}
}

func tlsFailureReason(err error) string {
	if err == nil {
		return ""
	}
	reason := err.Error()
	if len(reason) > 256 {
		reason = reason[:256]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, reason)
}
