package auth

import "testing"

func TestEvalARC(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		res := EvalARC([]Header{{Name: "From", Value: "a@example.com"}})
		if res.Result != "none" {
			t.Fatalf("result=%s", res.Result)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Seal", Value: "i=1; cv=none"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com"},
		}
		res := EvalARC(h)
		if res.Result != "fail" {
			t.Fatalf("result=%s", res.Result)
		}
	})
	t.Run("pass_structure", func(t *testing.T) {
		h := []Header{
			{Name: "ARC-Authentication-Results", Value: "i=1; mx=example"},
			{Name: "ARC-Message-Signature", Value: "i=1; d=example.com"},
			{Name: "ARC-Seal", Value: "i=1; cv=none; d=example.com"},
		}
		res := EvalARC(h)
		if res.Result != "pass" {
			t.Fatalf("result=%s reason=%s", res.Result, res.Reason)
		}
	})
}
