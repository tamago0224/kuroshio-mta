package mailauth

import "testing"

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
