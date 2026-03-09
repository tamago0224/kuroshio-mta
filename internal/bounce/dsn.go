package bounce

import (
	"bufio"
	"bytes"
	"strings"
)

type DSNReport struct {
	Recipient      string
	Action         string
	Status         string
	DiagnosticCode string
}

func ParseDSN(raw []byte) ([]DSNReport, error) {
	s := bufio.NewScanner(bytes.NewReader(raw))
	var reports []DSNReport
	cur := DSNReport{}
	hasAny := false
	flush := func() {
		if cur.Recipient == "" && cur.Action == "" && cur.Status == "" && cur.DiagnosticCode == "" {
			return
		}
		reports = append(reports, cur)
		cur = DSNReport{}
	}
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			flush()
			hasAny = false
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(line[idx+1:])
		hasAny = true
		switch key {
		case "final-recipient":
			cur.Recipient = parseRecipient(val)
		case "action":
			cur.Action = strings.ToLower(val)
		case "status":
			cur.Status = val
		case "diagnostic-code":
			cur.DiagnosticCode = val
		}
	}
	if hasAny {
		flush()
	}
	return reports, s.Err()
}

func parseRecipient(v string) string {
	if i := strings.Index(v, ";"); i >= 0 {
		v = v[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(v))
}
