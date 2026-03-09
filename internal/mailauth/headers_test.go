package mailauth

import (
	"reflect"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	t.Run("crlf", func(t *testing.T) {
		h, b, err := SplitMessage([]byte("From: a@example.com\r\n\r\nhello"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h != "From: a@example.com" || b != "hello" {
			t.Fatalf("unexpected split: h=%q b=%q", h, b)
		}
	})
	t.Run("lf", func(t *testing.T) {
		h, b, err := SplitMessage([]byte("From: a@example.com\n\nhello"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h != "From: a@example.com" || b != "hello" {
			t.Fatalf("unexpected split: h=%q b=%q", h, b)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		_, _, err := SplitMessage([]byte("From: a@example.com"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestParseHeadersAndLookup(t *testing.T) {
	h := "From: Alice <alice@example.com>\r\n" +
		"Subject: hello\r\n" +
		"\tworld\r\n" +
		"X-Test: one\r\n" +
		"X-Test: two"
	headers, err := ParseHeaders(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(headers) != 4 {
		t.Fatalf("len(headers)=%d, want 4", len(headers))
	}
	subject, ok := FirstHeader(headers, "subject")
	if !ok || subject != "hello world" {
		t.Fatalf("subject=(%q,%v)", subject, ok)
	}
	vals := HeaderValues(headers, "x-test")
	if !reflect.DeepEqual(vals, []string{"one", "two"}) {
		t.Fatalf("x-test=%v", vals)
	}
}
