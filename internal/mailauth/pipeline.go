package mailauth

import (
	"fmt"
	"net"
	"strings"
)

type SPFPolicy struct {
	HeloMode     string
	MailFromMode string
}

func DefaultSPFPolicy() SPFPolicy {
	return SPFPolicy{
		HeloMode:     "advisory",
		MailFromMode: "advisory",
	}
}

func normalizeSPFMode(v, def string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "advisory", "enforce":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return def
	}
}

func Evaluate(remoteIP net.IP, helo, mailFrom string, raw []byte) Result {
	return EvaluateWithPolicy(remoteIP, helo, mailFrom, raw, DefaultSPFPolicy())
}

func EvaluateWithPolicy(remoteIP net.IP, helo, mailFrom string, raw []byte, policy SPFPolicy) Result {
	headerPart, bodyPart, err := SplitMessage(raw)
	if err != nil {
		return Result{Action: ActionReject, Reason: "invalid message format"}
	}
	headers, err := ParseHeaders(headerPart)
	if err != nil {
		return Result{Action: ActionReject, Reason: "invalid headers"}
	}

	heloMode := normalizeSPFMode(policy.HeloMode, "advisory")
	mailFromMode := normalizeSPFMode(policy.MailFromMode, "advisory")

	spfHelo := SPFResult{Result: "none", Reason: "helo spf disabled"}
	if heloMode != "off" {
		spfHelo = EvalSPFHelo(remoteIP, helo)
	}
	spfMailFrom := SPFResult{Result: "none", Reason: "mailfrom spf disabled"}
	if mailFromMode != "off" {
		spfMailFrom = EvalSPFMailFrom(remoteIP, mailFrom, helo)
	}
	effectiveSPF := spfMailFrom
	if strings.TrimSpace(mailFrom) == "" || mailFromMode == "off" {
		effectiveSPF = spfHelo
	}

	dkim := EvalDKIM(headers, bodyPart)
	arc := EvalARC(headers)
	fromDomain := ExtractFromDomain(headers)
	dmarc := EvalDMARC(fromDomain, effectiveSPF, dkim)

	result := Result{
		SPF:         effectiveSPF,
		SPFHelo:     spfHelo,
		SPFMailFrom: spfMailFrom,
		DKIM:        dkim,
		ARC:         arc,
		DMARC:       dmarc,
		Action:      ActionAccept,
	}
	if heloMode == "enforce" && spfRejected(spfHelo.Result) {
		result.Action = ActionReject
		result.Reason = "spf helo policy"
		return result
	}
	if mailFromMode == "enforce" && strings.TrimSpace(mailFrom) != "" && spfRejected(spfMailFrom.Result) {
		result.Action = ActionReject
		result.Reason = "spf mailfrom policy"
		return result
	}
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

func spfRejected(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "fail", "softfail", "permerror", "temperror":
		return true
	default:
		return false
	}
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
