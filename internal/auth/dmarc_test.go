package auth

import "testing"

func TestExtractFromDomain(t *testing.T) {
	headers := []Header{{Name: "From", Value: "Alice <alice@example.com>"}}
	if got := ExtractFromDomain(headers); got != "example.com" {
		t.Fatalf("got=%q", got)
	}
}

func TestAligned(t *testing.T) {
	tests := []struct {
		name       string
		fromDomain string
		authDomain string
		mode       string
		want       bool
	}{
		{name: "strict exact", fromDomain: "example.com", authDomain: "example.com", mode: "s", want: true},
		{name: "strict subdomain no", fromDomain: "a.example.com", authDomain: "example.com", mode: "s", want: false},
		{name: "relaxed subdomain yes", fromDomain: "a.example.com", authDomain: "example.com", mode: "r", want: true},
		{name: "relaxed org domain co uk", fromDomain: "a.example.co.uk", authDomain: "b.example.co.uk", mode: "r", want: true},
		{name: "relaxed unrelated", fromDomain: "example.com", authDomain: "example.net", mode: "r", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aligned(tt.fromDomain, tt.authDomain, tt.mode); got != tt.want {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestOrganizationalDomain(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "a.example.com", want: "example.com"},
		{in: "a.example.co.uk", want: "example.co.uk"},
		{in: "example", want: "example"},
	}
	for _, tt := range tests {
		if got := organizationalDomain(tt.in); got != tt.want {
			t.Fatalf("in=%q got=%q want=%q", tt.in, got, tt.want)
		}
	}
}
