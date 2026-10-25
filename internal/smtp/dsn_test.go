package smtp

import (
	"bufio"
	"net"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestParseDSNEnvelopeParameters(t *testing.T) {
	mail, err := parseMailFrom("FROM:<sender@example.test> RET=HDRS ENVID=message+2Bid")
	if err != nil {
		t.Fatal(err)
	}
	if mail.DSN == nil || mail.DSN.Return != "HDRS" || mail.DSN.EnvelopeID != "message+2Bid" {
		t.Fatalf("MAIL DSN = %+v", mail.DSN)
	}
	rcpt, err := parseRcptToArgs("TO:<user@example.test> NOTIFY=SUCCESS,FAILURE ORCPT=rfc822;old+40example.test", "mx.test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rcpt.DSN.Notify, ",") != "SUCCESS,FAILURE" || rcpt.DSN.OriginalRecipient != "rfc822;old+40example.test" {
		t.Fatalf("RCPT DSN = %+v", rcpt.DSN)
	}
}

func TestParseDSNEnvelopeParametersRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		mail string
		rcpt string
	}{
		{name: "invalid RET", mail: "FROM:<sender@example.test> RET=BODY"},
		{name: "malformed ENVID xtext", mail: "FROM:<sender@example.test> ENVID=bad+2"},
		{name: "NEVER combined", rcpt: "TO:<user@example.test> NOTIFY=NEVER,FAILURE"},
		{name: "invalid NOTIFY", rcpt: "TO:<user@example.test> NOTIFY=BOUNCE"},
		{name: "missing ORCPT type", rcpt: "TO:<user@example.test> ORCPT=old@example.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.mail != "" {
				_, err = parseMailFrom(tt.mail)
			} else {
				_, err = parseRcptToArgs(tt.rcpt, "mx.test")
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDSNParametersAreQueued(t *testing.T) {
	q := &recordingQueue{}
	s := &Server{
		recipientPolicy: allowTestRecipients{},
		cfg:             config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1024},
		queue:           q,
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)
	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	readSMTPResponse(t, r)
	commands := []string{
		"EHLO client.example",
		"MAIL FROM:<sender@example.test> RET=HDRS ENVID=message+2Bid",
		"RCPT TO:<user@example.test> NOTIFY=SUCCESS,FAILURE ORCPT=rfc822;old+40example.test",
		"DATA",
	}
	for _, command := range commands {
		mustWriteSMTPLine(t, w, command)
		_, code := readSMTPResponse(t, r)
		if command == "DATA" {
			if code != 354 {
				t.Fatalf("%s code = %d", command, code)
			}
			break
		}
		if code != 250 {
			t.Fatalf("%s code = %d", command, code)
		}
	}
	mustWriteSMTPLine(t, w, "Subject: DSN\r\n\r\nbody\r\n.")
	if _, code := readSMTPResponse(t, r); code != 250 {
		t.Fatalf("DATA completion code = %d", code)
	}
	if len(q.msgs) != 1 || q.msgs[0].DSN == nil {
		t.Fatalf("queued messages = %+v", q.msgs)
	}
	options := q.msgs[0].DSN.Recipients["user@example.test"]
	if q.msgs[0].DSN.Return != "HDRS" || len(options.Notify) != 2 {
		t.Fatalf("queued DSN = %+v", q.msgs[0].DSN)
	}
}

func TestEHLOAdvertisesDSN(t *testing.T) {
	var output strings.Builder
	w := bufio.NewWriter(&output)
	writeEHLOResponse(w, "mx.test", 1024, false, false)
	if !strings.Contains(output.String(), "250-DSN\r\n") {
		t.Fatalf("EHLO = %q", output.String())
	}
}
