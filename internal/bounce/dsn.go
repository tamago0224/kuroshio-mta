package bounce

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
)

type DSNReport struct {
	Recipient         string
	OriginalRecipient string
	Action            string
	Status            string
	RemoteMTA         string
	DiagnosticCode    string
	LastAttemptDate   string
	FinalLogID        string
	WillRetryUntil    string
	ReportingMTA      string
	DSNGateway        string
	ReceivedFromMTA   string
	ArrivalDate       string
}

func ParseDSN(raw []byte) ([]DSNReport, error) {
	blocks, err := splitDSNBlocks(raw)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, nil
	}

	msgLevel, err := parseDSNBlock(blocks[0])
	if err != nil {
		return nil, err
	}
	base := DSNReport{
		ReportingMTA:    parseTypedValue(msgLevel["reporting-mta"]),
		DSNGateway:      parseTypedValue(msgLevel["dsn-gateway"]),
		ReceivedFromMTA: parseTypedValue(msgLevel["received-from-mta"]),
		ArrivalDate:     strings.TrimSpace(msgLevel["arrival-date"]),
	}
	if base.ReportingMTA == "" {
		return nil, errors.New("reporting-mta is required")
	}
	if base.ArrivalDate != "" {
		if _, err := mail.ParseDate(base.ArrivalDate); err != nil {
			return nil, fmt.Errorf("invalid arrival-date: %w", err)
		}
	}

	var reports []DSNReport
	for i := 1; i < len(blocks); i++ {
		fields, err := parseDSNBlock(blocks[i])
		if err != nil {
			return nil, err
		}
		r := base
		r.Recipient = parseRecipient(fields["final-recipient"])
		r.OriginalRecipient = parseRecipient(fields["original-recipient"])
		r.Action = strings.ToLower(strings.TrimSpace(fields["action"]))
		status, err := parseStatus(fields["status"])
		if err != nil {
			return nil, fmt.Errorf("recipient block %d: %w", i, err)
		}
		r.Status = status
		r.RemoteMTA = parseTypedValue(fields["remote-mta"])
		r.DiagnosticCode = parseTypedValue(fields["diagnostic-code"])
		r.LastAttemptDate = strings.TrimSpace(fields["last-attempt-date"])
		r.FinalLogID = parseTypedValue(fields["final-log-id"])
		r.WillRetryUntil = strings.TrimSpace(fields["will-retry-until"])

		if err := validateDSNRecipientReport(r); err != nil {
			return nil, fmt.Errorf("recipient block %d: %w", i, err)
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func splitDSNBlocks(raw []byte) ([][]string, error) {
	s := bufio.NewScanner(bytes.NewReader(raw))
	var blocks [][]string
	cur := make([]string, 0, 16)
	for s.Scan() {
		line := strings.TrimRight(s.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = make([]string, 0, 16)
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	return blocks, s.Err()
}

func parseDSNBlock(lines []string) (map[string]string, error) {
	fields := map[string]string{}
	var lastKey string
	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if lastKey == "" {
				return nil, errors.New("invalid folded line without previous header")
			}
			fields[lastKey] = fields[lastKey] + " " + strings.TrimSpace(line)
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid header line: %q", line)
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(line[idx+1:])
		fields[key] = val
		lastKey = key
	}
	return fields, nil
}

func validateDSNRecipientReport(r DSNReport) error {
	if r.Recipient == "" {
		return errors.New("final-recipient is required")
	}
	switch r.Action {
	case "failed", "delayed", "delivered", "relayed", "expanded":
	default:
		return fmt.Errorf("invalid action: %q", r.Action)
	}
	if err := validateActionStatusAlignment(r.Action, r.Status); err != nil {
		return err
	}
	if r.LastAttemptDate != "" {
		if _, err := mail.ParseDate(r.LastAttemptDate); err != nil {
			return fmt.Errorf("invalid last-attempt-date: %w", err)
		}
	}
	if r.WillRetryUntil != "" {
		if _, err := mail.ParseDate(r.WillRetryUntil); err != nil {
			return fmt.Errorf("invalid will-retry-until: %w", err)
		}
	}
	return nil
}

func parseStatus(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("status is required")
	}
	if idx := strings.Index(v, "("); idx >= 0 {
		v = strings.TrimSpace(v[:idx])
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid status: %q", v)
	}
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("invalid status: %q", v)
		}
		if len(part) > 1 && part[0] == '0' {
			return "", fmt.Errorf("invalid status: %q", v)
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 999 {
			return "", fmt.Errorf("invalid status: %q", v)
		}
	}
	return strings.Join(parts, "."), nil
}

func validateActionStatusAlignment(action, status string) error {
	statusClass := strings.SplitN(strings.TrimSpace(status), ".", 2)[0]
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "failed":
		if statusClass != "5" {
			return fmt.Errorf("action/status mismatch: failed requires 5.x.x, got %q", status)
		}
	case "delayed":
		if statusClass != "4" {
			return fmt.Errorf("action/status mismatch: delayed requires 4.x.x, got %q", status)
		}
	case "delivered", "relayed", "expanded":
		if statusClass != "2" {
			return fmt.Errorf("action/status mismatch: %s requires 2.x.x, got %q", action, status)
		}
	}
	return nil
}

func parseRecipient(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.Index(v, ";"); i >= 0 {
		v = v[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

func parseTypedValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.Index(v, ";"); i >= 0 {
		return strings.TrimSpace(v[i+1:])
	}
	return v
}
