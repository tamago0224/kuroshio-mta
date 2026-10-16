package delivery

import (
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// SMTPStage identifies the command or local policy that failed.
type SMTPStage string

const (
	StageConnect  SMTPStage = "connect"
	StageGreeting SMTPStage = "greeting"
	StageEHLO     SMTPStage = "ehlo"
	StageTLS      SMTPStage = "tls"
	StageAUTH     SMTPStage = "auth"
	StageMAIL     SMTPStage = "mail"
	StageRCPT     SMTPStage = "rcpt"
	StageDATA     SMTPStage = "data"
	StageDNS      SMTPStage = "dns"
)

type FailureCategory string

const (
	FailureInvalidRecipient FailureCategory = "invalid_recipient"
	FailureMailboxFull      FailureCategory = "mailbox_full"
	FailurePolicy           FailureCategory = "policy"
	FailureSpam             FailureCategory = "spam"
	FailureAuth             FailureCategory = "auth"
	FailureDNS              FailureCategory = "dns"
	FailureTransport        FailureCategory = "transport"
	FailureUnknown          FailureCategory = "unknown"
)

// FailureClassification describes the cause independently of SMTP retry policy.
// Suppress is deliberately limited to permanent RCPT 5.1.1 responses.
type FailureClassification struct {
	Category       FailureCategory
	Rule           string
	Stage          SMTPStage
	EnhancedStatus string
	Suppress       bool
}

type stageError struct {
	stage SMTPStage
	err   error
}

func (e *stageError) Error() string { return e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func atStage(stage SMTPStage, err error) error {
	if err == nil {
		return nil
	}
	var response *SMTPResponseError
	if errors.As(err, &response) {
		copy := *response
		copy.Stage = stage
		copy.EnhancedStatus = responseEnhancedStatus(copy.Code, copy.Line)
		return &copy
	}
	return &stageError{stage: stage, err: err}
}

var enhancedStatusPattern = regexp.MustCompile(`^[245]\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$`)

// Only a status at the start of reply text is meaningful. Conflicting or
// incomplete multiline statuses are not evidence for suppressing an address.
func responseEnhancedStatus(code int, line string) string {
	status := ""
	for _, part := range strings.Split(line, "\n") {
		part = strings.TrimSpace(part)
		if len(part) >= 4 && part[0] >= '0' && part[0] <= '9' && part[1] >= '0' && part[1] <= '9' && part[2] >= '0' && part[2] <= '9' && (part[3] == ' ' || part[3] == '-') {
			if part[:3] != strconv.Itoa(code) {
				return ""
			}
			part = part[4:]
		}
		fields := strings.Fields(part)
		if len(fields) == 0 || !enhancedStatusPattern.MatchString(fields[0]) || int(fields[0][0]-'0') != code/100 {
			return ""
		}
		if status != "" && status != fields[0] {
			return ""
		}
		status = fields[0]
	}
	return status
}

// These ordered rules use registered enhanced statuses. Add provider-specific
// rules here with a distinct stable rule ID and regression cases; generic 5xx
// text must never imply that an address is invalid. Spam has no generic rule:
// 5.7.1 can represent many different policies, not just a spam verdict.
var enhancedFailureRules = []struct {
	suffix   string
	category FailureCategory
	rule     string
}{
	{".2.2", FailureMailboxFull, "enhanced_mailbox_full"},
	{".7.8", FailureAuth, "enhanced_auth_credentials"},
	{".7.9", FailureAuth, "enhanced_auth_mechanism"},
	{".4.3", FailureDNS, "enhanced_directory_server"},
}

func ClassifyFailure(err error) FailureClassification {
	result := FailureClassification{Category: FailureUnknown, Rule: "unknown"}
	var staged *stageError
	if errors.As(err, &staged) {
		result.Stage = staged.stage
	}
	var response *SMTPResponseError
	if errors.As(err, &response) {
		result.Stage = response.Stage
		result.EnhancedStatus = responseEnhancedStatus(response.Code, response.Line)
		if response.Stage == StageTLS {
			result.Category = FailurePolicy
			result.Rule = "tls_policy"
			return result
		}
		if response.Stage == StageAUTH || response.Code == 530 || response.Code == 535 {
			result.Category = FailureAuth
			result.Rule = "smtp_auth"
			return result
		}
		if result.EnhancedStatus == "5.1.1" && response.Stage == StageRCPT && response.Permanent() {
			result.Category = FailureInvalidRecipient
			result.Rule = "rcpt_5_1_1"
			result.Suppress = true
			return result
		}
		if result.EnhancedStatus != "" {
			for _, rule := range enhancedFailureRules {
				if strings.HasSuffix(result.EnhancedStatus, rule.suffix) {
					result.Category = rule.category
					result.Rule = rule.rule
					return result
				}
			}
			switch strings.Split(result.EnhancedStatus, ".")[1] {
			case "7":
				result.Category = FailurePolicy
				result.Rule = "enhanced_policy"
			case "4":
				result.Category = FailureTransport
				result.Rule = "enhanced_network"
			}
		}
		return result
	}
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.As(err, &dns), result.Stage == StageDNS:
		result.Category = FailureDNS
		result.Rule = "dns_error"
	case result.Stage == StageTLS:
		result.Category = FailurePolicy
		result.Rule = "tls_error"
	case errors.As(err, &network), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		result.Category = FailureTransport
		result.Rule = "transport_error"
	}
	return result
}
