package queue

import (
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestKafkaMessageCodec(t *testing.T) {
	in := &model.Message{
		ID:              "m1",
		SigningIdentity: &model.SigningIdentity{Domain: "example.com"},
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"user@example.net"},
		Data:            []byte("Subject: hi\r\n\r\nhello"),
		NextAttempt:     time.Now().UTC().Truncate(time.Second),
	}
	b, err := marshalMessage(in)
	if err != nil {
		t.Fatalf("marshalMessage: %v", err)
	}
	out, err := unmarshalMessage(b)
	if err != nil {
		t.Fatalf("unmarshalMessage: %v", err)
	}
	if out.SigningIdentity == nil || out.SigningIdentity.Domain != "example.com" {
		t.Fatal("signing identity lost in Kafka codec")
	}
	if out.ID != in.ID || out.MailFrom != in.MailFrom || len(out.RcptTo) != 1 {
		t.Fatalf("decoded message mismatch: %+v", out)
	}
}

func TestKafkaMessageCodecRejectsEmptyID(t *testing.T) {
	if _, err := unmarshalMessage([]byte(`{"mail_from":"a@example.com"}`)); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestLegacyKafkaMessageHasUnknownSigningOrigin(t *testing.T) {
	msg, err := unmarshalMessage([]byte(`{"id":"legacy","mail_from":"sender@example.com"}`))
	if err != nil || msg.SigningIdentity != nil {
		t.Fatalf("legacy identity: %+v %v", msg, err)
	}
}

func TestUnsignedIngressIdentitySurvivesKafkaCodec(t *testing.T) {
	encoded, err := marshalMessage(&model.Message{ID: "unsigned", SigningIdentity: &model.SigningIdentity{}})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := unmarshalMessage(encoded)
	if err != nil || restored.SigningIdentity == nil || restored.SigningIdentity.Domain != "" {
		t.Fatal("explicit unsigned origin became legacy unknown origin")
	}
}
