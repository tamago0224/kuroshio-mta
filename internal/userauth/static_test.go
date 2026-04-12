package userauth

import "testing"

func TestNewStaticAndAuthenticatePassword(t *testing.T) {
	b, err := NewStatic("alice@example.com:s3cr3t, bob@example.com:pass")
	if err != nil {
		t.Fatalf("new static: %v", err)
	}
	principal, ok := b.AuthenticatePassword("alice@example.com", "s3cr3t")
	if !ok {
		t.Fatal("alice should authenticate")
	}
	if principal.Username != "alice@example.com" {
		t.Fatalf("principal username=%q", principal.Username)
	}
	if _, ok := b.AuthenticatePassword("ALICE@EXAMPLE.COM", "s3cr3t"); !ok {
		t.Fatal("username lookup should be case-insensitive")
	}
	if _, ok := b.AuthenticatePassword("bob@example.com", "wrong"); ok {
		t.Fatal("wrong password must fail")
	}
	if _, ok := b.AuthenticatePassword("charlie@example.com", "pass"); ok {
		t.Fatal("unknown user must fail")
	}
}

func TestNewStaticRejectsInvalidEntry(t *testing.T) {
	if _, err := NewStatic("alice@example.com"); err == nil {
		t.Fatal("invalid entry must fail")
	}
}
