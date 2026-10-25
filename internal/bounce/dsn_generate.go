package bounce

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

var enhancedStatusInTextPattern = regexp.MustCompile(`([245]\.\d+\.\d+)`)

func BuildFailureDSN(original *model.Message, failedRcpt, diagnostic, hostname string, now time.Time) (*model.Message, error) {
	return buildDSN(original, failedRcpt, "failed", "5.0.0", diagnostic, hostname, now)
}

func BuildDelayDSN(original *model.Message, failedRcpt, diagnostic, hostname string, now time.Time) (*model.Message, error) {
	return buildDSN(original, failedRcpt, "delayed", "4.0.0", diagnostic, hostname, now)
}

func BuildSuccessDSN(original *model.Message, deliveredRcpt, diagnostic, hostname string, now time.Time) (*model.Message, error) {
	return buildDSN(original, deliveredRcpt, "delivered", "2.0.0", diagnostic, hostname, now)
}

func ShouldNotify(original *model.Message, rcpt, event string) bool {
	if original == nil || original.DSN == nil {
		return event == "FAILURE" || event == "DELAY"
	}
	options, ok := original.DSN.Recipients[strings.ToLower(strings.TrimSpace(rcpt))]
	if !ok || len(options.Notify) == 0 {
		return event == "FAILURE" || event == "DELAY"
	}
	for _, requested := range options.Notify {
		if requested == "NEVER" {
			return false
		}
		if strings.EqualFold(requested, event) {
			return true
		}
	}
	return false
}

func buildDSN(original *model.Message, failedRcpt, action, defaultStatus, diagnostic, hostname string, now time.Time) (*model.Message, error) {
	if original == nil {
		return nil, fmt.Errorf("original message is nil")
	}
	mailFrom := strings.TrimSpace(original.MailFrom)
	if mailFrom == "" || mailFrom == "<>" {
		return nil, fmt.Errorf("original sender is empty")
	}
	if hasAutoSubmittedNonNo(original.Data) {
		return nil, fmt.Errorf("original message is auto-submitted")
	}
	if !strings.Contains(mailFrom, "@") {
		return nil, fmt.Errorf("original sender is invalid")
	}
	recipient := strings.ToLower(strings.TrimSpace(failedRcpt))
	if recipient == "" || !strings.Contains(recipient, "@") {
		return nil, fmt.Errorf("failed recipient is invalid")
	}
	host := strings.TrimSpace(hostname)
	if host == "" {
		host = "kuroshio.local"
	}
	status := defaultStatus
	if s, ok := extractEnhancedStatus(diagnostic); ok {
		status = s
	}

	boundary := fmt.Sprintf("dsn-%d", now.UTC().UnixNano())
	human := "Your message could not be delivered."
	if action == "delayed" {
		human = "Your message delivery is delayed and will be retried."
	} else if action == "delivered" {
		human = "Your message was delivered successfully."
	}
	lines := []string{
		fmt.Sprintf("From: MAILER-DAEMON@%s", host),
		fmt.Sprintf("To: %s", mailFrom),
		fmt.Sprintf("Subject: Delivery Status Notification (%s)", strings.ToUpper(action)),
		"Auto-Submitted: auto-generated",
		"MIME-Version: 1.0",
		fmt.Sprintf(`Content-Type: multipart/report; report-type=delivery-status; boundary="%s"`, boundary),
		"",
		fmt.Sprintf("--%s", boundary),
		"Content-Type: text/plain; charset=utf-8",
		"",
		human,
		"",
		fmt.Sprintf("--%s", boundary),
		"Content-Type: message/delivery-status",
		"",
		fmt.Sprintf("Reporting-MTA: dns; %s", host),
		fmt.Sprintf("Arrival-Date: %s", now.UTC().Format(time.RFC1123Z)),
	}
	if original.DSN != nil && original.DSN.EnvelopeID != "" {
		lines = append(lines, "Original-Envelope-Id: "+original.DSN.EnvelopeID)
	}
	lines = append(lines, "")
	if original.DSN != nil {
		if options, ok := original.DSN.Recipients[recipient]; ok && options.OriginalRecipient != "" {
			lines = append(lines, "Original-Recipient: "+options.OriginalRecipient)
		}
	}
	lines = append(lines,
		fmt.Sprintf("Final-Recipient: rfc822; %s", recipient),
		fmt.Sprintf("Action: %s", action),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Diagnostic-Code: smtp; %s", strings.TrimSpace(diagnostic)),
		"",
	)
	if original.DSN != nil {
		returnMode := original.DSN.Return
		if returnMode == "" {
			returnMode = "FULL"
		}
		contentType := "message/rfc822"
		content := original.Data
		if returnMode == "HDRS" {
			contentType = "text/rfc822-headers"
			content = messageHeaders(original.Data)
		}
		lines = append(lines,
			fmt.Sprintf("--%s", boundary),
			"Content-Type: "+contentType,
			"",
			strings.TrimRight(string(content), "\r\n"),
			"",
		)
	}
	lines = append(lines, fmt.Sprintf("--%s--", boundary), "")
	data := strings.Join(lines, "\r\n")

	return &model.Message{
		ID:           fmt.Sprintf("dsn-%d", now.UTC().UnixNano()),
		CreatedAt:    now.UTC(),
		UpdatedAt:    now.UTC(),
		MailFrom:     "",
		RcptTo:       []string{mailFrom},
		Data:         []byte(data),
		Attempts:     0,
		NextAttempt:  now.UTC(),
		Tenant:       "system",
		TrafficClass: "system",
	}, nil
}

func messageHeaders(raw []byte) []byte {
	if idx := strings.Index(string(raw), "\r\n\r\n"); idx >= 0 {
		return raw[:idx]
	}
	if idx := strings.Index(string(raw), "\n\n"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

func extractEnhancedStatus(v string) (string, bool) {
	m := enhancedStatusInTextPattern.FindStringSubmatch(v)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func hasAutoSubmittedNonNo(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	header := string(raw)
	if idx := strings.Index(header, "\r\n\r\n"); idx >= 0 {
		header = header[:idx]
	} else if idx := strings.Index(header, "\n\n"); idx >= 0 {
		header = header[:idx]
	}
	lines := strings.Split(strings.ReplaceAll(header, "\r\n", "\n"), "\n")
	curName := ""
	curValue := ""
	flush := func() bool {
		if !strings.EqualFold(curName, "Auto-Submitted") {
			return false
		}
		v := strings.TrimSpace(strings.ToLower(curValue))
		return v != "" && v != "no"
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if curName != "" {
				curValue += " " + strings.TrimSpace(line)
			}
			continue
		}
		if curName != "" && flush() {
			return true
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			curName = ""
			curValue = ""
			continue
		}
		curName = strings.TrimSpace(parts[0])
		curValue = strings.TrimSpace(parts[1])
	}
	return curName != "" && flush()
}
