package util

import "testing"

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "angle brackets", in: "<User@Example.COM>", want: "user@example.com"},
		{name: "plain address", in: "postmaster@example.com", want: "postmaster@example.com"},
		{name: "empty reverse path", in: "<>", want: ""},
		{name: "missing at", in: "postmaster", wantErr: true},
		{name: "whitespace invalid", in: "<a b@example.com>", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePath(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestDomainOf(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "alice@example.com", want: "example.com", ok: true},
		{in: "invalid", want: "", ok: false},
		{in: "a@", want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := DomainOf(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("in=%q got=(%q,%v) want=(%q,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
