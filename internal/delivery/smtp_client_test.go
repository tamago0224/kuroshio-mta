package delivery

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestExpect2xxReturnsSMTPResponseError(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("550 mailbox unavailable\r\n"))
	err := expect2xx(r)
	if err == nil {
		t.Fatal("expected error")
	}
	var smtpErr *SMTPResponseError
	if !errors.As(err, &smtpErr) {
		t.Fatalf("expected SMTPResponseError, got %T", err)
	}
	if smtpErr.Code != 550 || !smtpErr.Permanent() {
		t.Fatalf("unexpected smtpErr: %+v", smtpErr)
	}
}

func TestExpectCodeReturnsSMTPResponseError(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("421 service not available\r\n"))
	err := expectCode(r, 354)
	if err == nil {
		t.Fatal("expected error")
	}
	var smtpErr *SMTPResponseError
	if !errors.As(err, &smtpErr) {
		t.Fatalf("expected SMTPResponseError, got %T", err)
	}
	if smtpErr.Code != 421 || !smtpErr.Temporary() {
		t.Fatalf("unexpected smtpErr: %+v", smtpErr)
	}
}
