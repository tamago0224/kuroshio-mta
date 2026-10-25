package model

import (
	"encoding/json"
	"testing"
)

func TestMessageLegacyJSONLeavesDSNUnset(t *testing.T) {
	var msg Message
	if err := json.Unmarshal([]byte(`{"id":"legacy","mail_from":"sender@example.test","rcpt_to":["user@example.test"]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.DSN != nil {
		t.Fatalf("legacy DSN = %+v, want nil", msg.DSN)
	}
}

func TestMessageDSNJSONRoundTrip(t *testing.T) {
	want := Message{
		ID:       "dsn",
		MailFrom: "sender@example.test",
		RcptTo:   []string{"user@example.test"},
		DSN: &DSN{
			Return:     "HDRS",
			EnvelopeID: "id+2B1",
			Recipients: map[string]DSNRecipientOptions{
				"user@example.test": {Notify: []string{"SUCCESS"}, OriginalRecipient: "rfc822;old+40example.test"},
			},
		},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.DSN == nil || got.DSN.EnvelopeID != want.DSN.EnvelopeID || got.DSN.Recipients["user@example.test"].OriginalRecipient == "" {
		t.Fatalf("round trip DSN = %+v", got.DSN)
	}
}
