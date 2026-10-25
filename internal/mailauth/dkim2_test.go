package mailauth

import (
	"context"
	"testing"
)

func TestEvaluateDKIM2NoHeadersIsSeparateObservation(t *testing.T) {
	result := EvaluateDKIM2(context.Background(), []byte("From: sender@example.test\r\n\r\nbody\r\n"), "sender@example.test", []string{"user@recipient.test"})
	if result.Result != "none" || result.Revision == "" {
		t.Fatalf("result=%+v", result)
	}
}
