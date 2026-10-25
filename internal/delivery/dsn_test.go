package delivery

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDeliverTransactionForwardsDSNParameters(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	commands := make(chan []string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		w := bufio.NewWriter(serverConn)
		got := make([]string, 0, 3)
		for range 3 {
			line, _ := r.ReadString('\n')
			got = append(got, strings.TrimSpace(line))
			code := "250 ok\r\n"
			if strings.HasPrefix(strings.ToUpper(line), "DATA") {
				code = "354 continue\r\n"
			}
			_, _ = fmt.Fprint(w, code)
			_ = w.Flush()
		}
		for {
			line, _ := r.ReadString('\n')
			if line == ".\r\n" {
				break
			}
		}
		_, _ = fmt.Fprint(w, "250 queued\r\n")
		_ = w.Flush()
		commands <- got
	}()
	c := NewClient(config.Config{Hostname: "sender.test"})
	session := &smtpSession{conn: clientConn, r: bufio.NewReader(clientConn), w: bufio.NewWriter(clientConn), dsn: true}
	msg := &model.Message{
		MailFrom: "sender@example.test",
		Data:     []byte("Subject: test\r\n\r\nbody"),
		DSN: &model.DSN{
			Return:     "HDRS",
			EnvelopeID: "id+2B1",
			Recipients: map[string]model.DSNRecipientOptions{
				"user@example.test": {Notify: []string{"SUCCESS", "FAILURE"}, OriginalRecipient: "rfc822;old+40example.test"},
			},
		},
	}
	reusable, err := c.deliverTransaction(context.Background(), session, msg, "user@example.test")
	if err != nil || !reusable {
		t.Fatalf("deliverTransaction reusable=%v err=%v", reusable, err)
	}
	select {
	case got := <-commands:
		if got[0] != "MAIL FROM:<sender@example.test> RET=HDRS ENVID=id+2B1" {
			t.Fatalf("MAIL = %q", got[0])
		}
		if got[1] != "RCPT TO:<user@example.test> NOTIFY=SUCCESS,FAILURE ORCPT=rfc822;old+40example.test" {
			t.Fatalf("RCPT = %q", got[1])
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP peer did not finish")
	}
}
