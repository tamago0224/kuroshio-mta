package mailauth

import (
	"fmt"
	"net"
	"strings"
)

func Evaluate(remoteIP net.IP, helo, mailFrom string, raw []byte) Result {
	headerPart, bodyPart, err := SplitMessage(raw)
	if err != nil {
		return Result{Action: ActionReject, Reason: "invalid message format"}
	}
	headers, err := ParseHeaders(headerPart)
	if err != nil {
		return Result{Action: ActionReject, Reason: "invalid headers"}
	}

	spf := EvalSPF(remoteIP, mailFrom, helo)
	dkim := EvalDKIM(headers, bodyPart)
	arc := EvalARC(headers)
	fromDomain := ExtractFromDomain(headers)
	dmarc := EvalDMARC(fromDomain, spf, dkim)

	result := Result{SPF: spf, DKIM: dkim, ARC: arc, DMARC: dmarc, Action: ActionAccept}
	switch strings.ToLower(dmarc.Result) {
	case "pass", "none":
		result.Action = ActionAccept
	case "fail":
		switch strings.ToLower(dmarc.Policy) {
		case "reject":
			result.Action = ActionReject
			result.Reason = "dmarc reject policy"
		case "quarantine":
			result.Action = ActionQuarantine
			result.Reason = "dmarc quarantine policy"
		default:
			result.Action = ActionAccept
		}
	default:
		result.Action = ActionAccept
	}
	return result
}

func BuildAuthResultsHeader(hostname string, res Result, mailFrom string) string {
	dkimDom := ""
	for _, s := range res.DKIM.Sigs {
		if s.Result == "pass" {
			dkimDom = s.Domain
			break
		}
	}
	parts := []string{
		hostname,
		fmt.Sprintf("spf=%s smtp.mailfrom=%s", res.SPF.Result, mailFrom),
	}
	if dkimDom != "" {
		parts = append(parts, fmt.Sprintf("dkim=%s header.d=%s", res.DKIM.Result, dkimDom))
	} else {
		parts = append(parts, fmt.Sprintf("dkim=%s", res.DKIM.Result))
	}
	parts = append(parts, fmt.Sprintf("dmarc=%s header.from=%s", res.DMARC.Result, res.DMARC.Domain))
	parts = append(parts, fmt.Sprintf("arc=%s", res.ARC.Result))
	return "Authentication-Results: " + strings.Join(parts, "; ")
}

func InjectHeaders(raw []byte, add []string) []byte {
	h, b, err := SplitMessage(raw)
	if err != nil {
		return raw
	}
	var sb strings.Builder
	for _, x := range add {
		sb.WriteString(x)
		sb.WriteString("\r\n")
	}
	sb.WriteString(h)
	sb.WriteString("\r\n\r\n")
	sb.WriteString(b)
	return []byte(sb.String())
}
