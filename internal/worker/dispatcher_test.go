package worker

import "testing"

func TestBackoff(t *testing.T) {
	cases := map[int]string{
		0: "5m0s",
		1: "30m0s",
		2: "2h0m0s",
		3: "6h0m0s",
		4: "24h0m0s",
	}
	for attempts, want := range cases {
		if got := backoff(attempts).String(); got != want {
			t.Fatalf("attempts=%d got=%s want=%s", attempts, got, want)
		}
	}
}

func TestIsPermanent(t *testing.T) {
	if !isPermanent("smtp reject code=550") {
		t.Fatal("expected permanent for 5xx")
	}
	if isPermanent("temporary timeout") {
		t.Fatal("temporary error should not be permanent")
	}
}
