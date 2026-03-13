package mailauth

import (
	"strings"
	"testing"
	"time"
)

func TestAggregateDKIMResults(t *testing.T) {
	tests := []struct {
		name    string
		results []DKIMSigResult
		want    string
	}{
		{
			name:    "empty is none",
			results: nil,
			want:    "none",
		},
		{
			name: "any pass wins",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "pass"},
				{Result: "temperror"},
			},
			want: "pass",
		},
		{
			name: "temperror beats fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "temperror"},
			},
			want: "temperror",
		},
		{
			name: "permerror beats fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "permerror"},
			},
			want: "permerror",
		},
		{
			name: "temperror beats permerror",
			results: []DKIMSigResult{
				{Result: "permerror"},
				{Result: "temperror"},
			},
			want: "temperror",
		},
		{
			name: "all fail stays fail",
			results: []DKIMSigResult{
				{Result: "fail"},
				{Result: "fail"},
			},
			want: "fail",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateDKIMResults(tc.results); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestEvalDKIMWithoutSignatureReturnsNone(t *testing.T) {
	headers := []Header{{Name: "From", Value: "sender@example.com"}}
	got := EvalDKIM(headers, "body")
	if got.Result != "none" {
		t.Fatalf("result=%q want=none", got.Result)
	}
	if len(got.Sigs) != 0 {
		t.Fatalf("len(sigs)=%d want=0", len(got.Sigs))
	}
}

func TestCanonicalizeBodyWithLength(t *testing.T) {
	got, err := canonicalizeBodyWithLength("a\r\nb\r\n", "simple", "3")
	if err != nil {
		t.Fatalf("canonicalizeBodyWithLength: %v", err)
	}
	if string(got) != "a\r\n" {
		t.Fatalf("got=%q want=%q", string(got), "a\r\n")
	}
}

func TestCanonicalizeBodyWithLengthRejectsInvalidTag(t *testing.T) {
	if _, err := canonicalizeBodyWithLength("a\r\n", "simple", "-1"); err == nil {
		t.Fatal("expected invalid l tag error")
	}
	if _, err := canonicalizeBodyWithLength("a\r\n", "simple", "999"); err == nil {
		t.Fatal("expected oversized l tag error")
	}
}

func TestValidateDKIMTimeTags(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	if err := validateDKIMTimeTags(map[string]string{"t": "100", "x": "300"}, now); err != nil {
		t.Fatalf("validateDKIMTimeTags valid: %v", err)
	}
	if err := validateDKIMTimeTags(map[string]string{"t": "300", "x": "200"}, now); err == nil {
		t.Fatal("expected x earlier than t")
	}
	if err := validateDKIMTimeTags(map[string]string{"x": "150"}, now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired signature error, got=%v", err)
	}
	if err := validateDKIMTimeTags(map[string]string{"t": "bad"}, now); err == nil {
		t.Fatal("expected invalid t tag")
	}
}
