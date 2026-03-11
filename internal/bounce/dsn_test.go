package bounce

import "testing"

func TestParseDSN(t *testing.T) {
	raw := []byte(
		"Reporting-MTA: dns; mx.example.net\r\n" +
			"Arrival-Date: Tue, 12 Mar 2024 09:30:00 +0000\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user@example.com\r\n" +
			"Action: failed\r\n" +
			"Status: 5.1.1\r\n" +
			"Diagnostic-Code: smtp; 550 5.1.1 User unknown\r\n",
	)
	reports, err := ParseDSN(raw)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len=%d", len(reports))
	}
	r := reports[0]
	if r.Recipient != "user@example.com" || r.Action != "failed" || r.Status != "5.1.1" {
		t.Fatalf("unexpected report: %+v", r)
	}
	if r.ReportingMTA != "mx.example.net" {
		t.Fatalf("unexpected reporting-mta: %+v", r)
	}
}

func TestParseDSN_MultipleRecipientBlocks(t *testing.T) {
	raw := []byte(
		"Reporting-MTA: dns; mx.example.net\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user1@example.com\r\n" +
			"Action: failed\r\n" +
			"Status: 5.1.1\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user2@example.com\r\n" +
			"Action: delayed\r\n" +
			"Status: 4.2.0\r\n",
	)
	reports, err := ParseDSN(raw)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("len=%d", len(reports))
	}
	if reports[1].Recipient != "user2@example.com" || reports[1].Action != "delayed" || reports[1].Status != "4.2.0" {
		t.Fatalf("unexpected second report: %+v", reports[1])
	}
}

func TestParseDSN_StrictValidationError(t *testing.T) {
	raw := []byte(
		"Reporting-MTA: dns; mx.example.net\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user@example.com\r\n" +
			"Action: invalid-action\r\n" +
			"Status: bad-status\r\n",
	)
	if _, err := ParseDSN(raw); err == nil {
		t.Fatal("expected strict validation error")
	}
}

func TestParseDSN_ActionStatusAlignmentError(t *testing.T) {
	raw := []byte(
		"Reporting-MTA: dns; mx.example.net\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user@example.com\r\n" +
			"Action: failed\r\n" +
			"Status: 4.1.1\r\n",
	)
	if _, err := ParseDSN(raw); err == nil {
		t.Fatal("expected action/status alignment error")
	}
}

func TestParseDSN_FoldedHeader(t *testing.T) {
	raw := []byte(
		"Reporting-MTA: dns; mx.example.net\r\n" +
			"\r\n" +
			"Final-Recipient: rfc822; user@example.com\r\n" +
			"Action: failed\r\n" +
			"Status: 5.1.1\r\n" +
			"Diagnostic-Code: smtp; 550 5.1.1\r\n" +
			" User unknown\r\n",
	)
	reports, err := ParseDSN(raw)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len=%d", len(reports))
	}
	if reports[0].DiagnosticCode != "550 5.1.1 User unknown" {
		t.Fatalf("unexpected diagnostic-code: %q", reports[0].DiagnosticCode)
	}
}
