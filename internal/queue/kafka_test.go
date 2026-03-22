package queue

import (
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestKafkaMessageCodec(t *testing.T) {
	in := &model.Message{
		ID:          "m1",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
		MailFrom:    "sender@example.com",
		RcptTo:      []string{"user@example.net"},
		Data:        []byte("Subject: hi\r\n\r\nhello"),
		NextAttempt: time.Now().UTC().Truncate(time.Second),
	}
	b, err := marshalMessage(in)
	if err != nil {
		t.Fatalf("marshalMessage: %v", err)
	}
	out, err := unmarshalMessage(b)
	if err != nil {
		t.Fatalf("unmarshalMessage: %v", err)
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
