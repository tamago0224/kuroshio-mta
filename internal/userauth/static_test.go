package userauth

import "testing"

func TestNewStaticAndValidate(t *testing.T) {
	b, err := NewStatic("alice@example.com:s3cr3t, bob@example.com:pass")
	if err != nil {
		t.Fatalf("new static: %v", err)
	}
	if !b.Validate("alice@example.com", "s3cr3t") {
		t.Fatal("alice should authenticate")
	}
	if !b.Validate("ALICE@EXAMPLE.COM", "s3cr3t") {
		t.Fatal("username lookup should be case-insensitive")
	}
	if b.Validate("bob@example.com", "wrong") {
		t.Fatal("wrong password must fail")
	}
	if b.Validate("charlie@example.com", "pass") {
		t.Fatal("unknown user must fail")
	}
}

func TestNewStaticRejectsInvalidEntry(t *testing.T) {
	if _, err := NewStatic("alice@example.com"); err == nil {
		t.Fatal("invalid entry must fail")
	}
}
