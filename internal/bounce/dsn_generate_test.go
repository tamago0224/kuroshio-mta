package bounce

import (
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestBuildFailureDSN(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	orig := &model.Message{
		ID:       "m1",
		MailFrom: "sender@example.com",
	}
	dsn, err := BuildFailureDSN(orig, "user@example.net", "550 5.1.1 User unknown", "mx.example.com", now)
	if err != nil {
		t.Fatalf("BuildFailureDSN: %v", err)
	}
	if dsn.MailFrom != "" {
		t.Fatalf("dsn mail from should be null reverse-path, got=%q", dsn.MailFrom)
	}
	if len(dsn.RcptTo) != 1 || dsn.RcptTo[0] != "sender@example.com" {
		t.Fatalf("unexpected rcpt: %+v", dsn.RcptTo)
	}
	body := string(dsn.Data)
	if !strings.Contains(body, "Action: failed") || !strings.Contains(body, "Status: 5.1.1") {
		t.Fatalf("unexpected dsn body: %q", body)
	}
	if !strings.Contains(body, "Auto-Submitted: auto-generated") {
		t.Fatalf("dsn must include auto-generated header: %q", body)
	}
}

func TestBuildDelayDSN(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	orig := &model.Message{
		ID:       "m1",
		MailFrom: "sender@example.com",
	}
	dsn, err := BuildDelayDSN(orig, "user@example.net", "451 temporary failure", "mx.example.com", now)
	if err != nil {
		t.Fatalf("BuildDelayDSN: %v", err)
	}
	body := string(dsn.Data)
	if !strings.Contains(body, "Action: delayed") || !strings.Contains(body, "Status: 4.0.0") {
		t.Fatalf("unexpected delayed dsn body: %q", body)
	}
}

func TestBuildDSNHonorsRFC3461Metadata(t *testing.T) {
	now := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	orig := &model.Message{
		MailFrom: "sender@example.com",
		Data:     []byte("Subject: original\r\nX-Test: yes\r\n\r\nsecret body\r\n"),
		DSN: &model.DSN{
			Return:     "HDRS",
			EnvelopeID: "id+2B1",
			Recipients: map[string]model.DSNRecipientOptions{
				"user@example.net": {Notify: []string{"FAILURE"}, OriginalRecipient: "rfc822;old+40example.net"},
			},
		},
	}
	dsn, err := BuildFailureDSN(orig, "user@example.net", "550 failed", "mx.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	body := string(dsn.Data)
	for _, want := range []string{"Original-Envelope-Id: id+2B1", "Original-Recipient: rfc822;old+40example.net", "Content-Type: text/rfc822-headers", "Subject: original"} {
		if !strings.Contains(body, want) {
			t.Fatalf("DSN missing %q: %q", want, body)
		}
	}
	if strings.Contains(body, "secret body") {
		t.Fatalf("RET=HDRS included body: %q", body)
	}
}

func TestShouldNotify(t *testing.T) {
	msg := &model.Message{DSN: &model.DSN{Recipients: map[string]model.DSNRecipientOptions{
		"success@example.net": {Notify: []string{"SUCCESS"}},
		"never@example.net":   {Notify: []string{"NEVER"}},
	}}}
	if !ShouldNotify(msg, "success@example.net", "SUCCESS") || ShouldNotify(msg, "success@example.net", "FAILURE") {
		t.Fatal("SUCCESS selection was not honored")
	}
	if ShouldNotify(msg, "never@example.net", "FAILURE") {
		t.Fatal("NEVER selection was not honored")
	}
	if !ShouldNotify(&model.Message{}, "legacy@example.net", "FAILURE") {
		t.Fatal("legacy failure default changed")
	}
}

func TestBuildDSNRejectsEmptySender(t *testing.T) {
	orig := &model.Message{MailFrom: ""}
	if _, err := BuildFailureDSN(orig, "user@example.net", "550 failed", "mx.example.com", time.Now()); err == nil {
		t.Fatal("expected error for empty sender")
	}
}

func TestBuildDSNRejectsNullReversePath(t *testing.T) {
	orig := &model.Message{MailFrom: "<>"}
	if _, err := BuildFailureDSN(orig, "user@example.net", "550 failed", "mx.example.com", time.Now()); err == nil {
		t.Fatal("expected error for null reverse-path sender")
	}
}

func TestBuildDSNRejectsAutoSubmittedMessage(t *testing.T) {
	orig := &model.Message{
		MailFrom: "sender@example.com",
		Data: []byte(strings.Join([]string{
			"From: sender@example.com",
			"To: user@example.net",
			"Auto-Submitted: auto-generated",
			"",
			"payload",
		}, "\r\n")),
	}
	if _, err := BuildFailureDSN(orig, "user@example.net", "550 failed", "mx.example.com", time.Now()); err == nil {
		t.Fatal("expected error for auto-submitted original message")
	}
}

func TestBuildDSNAllowsAutoSubmittedNo(t *testing.T) {
	orig := &model.Message{
		MailFrom: "sender@example.com",
		Data: []byte(strings.Join([]string{
			"From: sender@example.com",
			"To: user@example.net",
			"Auto-Submitted: no",
			"",
			"payload",
		}, "\r\n")),
	}
	if _, err := BuildFailureDSN(orig, "user@example.net", "550 failed", "mx.example.com", time.Now()); err != nil {
		t.Fatalf("auto-submitted=no should be allowed: %v", err)
	}
}
