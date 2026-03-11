package logging

import "testing"

func TestMaskEmail(t *testing.T) {
	if got := MaskEmail("alice@example.com"); got != "a***e@example.com" {
		t.Fatalf("mask email=%q", got)
	}
	if got := MaskEmail("ab@example.com"); got != "***@example.com" {
		t.Fatalf("mask short email=%q", got)
	}
}

func TestMaskIP(t *testing.T) {
	if got := MaskIP("192.168.1.2"); got != "192.168.x.x" {
		t.Fatalf("mask ip=%q", got)
	}
	if got := MaskIP("2001:db8::1"); got == "" {
		t.Fatal("mask ipv6 should not be empty")
	}
}
