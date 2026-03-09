package bounce

import "testing"

func TestParseDSN(t *testing.T) {
	raw := []byte("Final-Recipient: rfc822; user@example.com\r\nAction: failed\r\nStatus: 5.1.1\r\nDiagnostic-Code: smtp; 550 5.1.1 User unknown\r\n")
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
}
