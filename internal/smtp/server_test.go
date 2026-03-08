package smtp

import (
	"bufio"
	"strings"
	"testing"
)

func TestSplitVerb(t *testing.T) {
	verb, arg := splitVerb("MAIL FROM:<a@example.com>")
	if verb != "MAIL" || arg != "FROM:<a@example.com>" {
		t.Fatalf("verb=%q arg=%q", verb, arg)
	}
}

func TestParseMailFrom(t *testing.T) {
	got, err := parseMailFrom("FROM:<Alice@Example.com>")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "alice@example.com" {
		t.Fatalf("got=%q", got)
	}
	if _, err := parseMailFrom("TO:<alice@example.com>"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRcptTo(t *testing.T) {
	got, err := parseRcptTo("TO:<Bob@Example.com>")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "bob@example.com" {
		t.Fatalf("got=%q", got)
	}
	if _, err := parseRcptTo("TO:<>"); err == nil {
		t.Fatal("expected error for empty rcpt")
	}
}

func TestReadData(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("hello\r\n..escaped\r\n.\r\n"))
	data, err := readData(in, 1024)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "hello\r\n.escaped\r\n"
	if string(data) != want {
		t.Fatalf("got=%q want=%q", string(data), want)
	}
}

func TestReadDataTooLarge(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("hello\r\nworld\r\n.\r\n"))
	_, err := readData(in, 5)
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestParseRemoteIP(t *testing.T) {
	if got := parseRemoteIP("127.0.0.1:25"); got == nil || got.String() != "127.0.0.1" {
		t.Fatalf("got=%v", got)
	}
	if got := parseRemoteIP("2001:db8::1"); got == nil || got.String() != "2001:db8::1" {
		t.Fatalf("got=%v", got)
	}
}
