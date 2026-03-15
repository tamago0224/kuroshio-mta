package main

import "testing"

func TestPercentile(t *testing.T) {
	xs := []float64{10, 20, 30, 40, 50}
	if got := percentile(xs, 0); got != 10 {
		t.Fatalf("p0=%v want=10", got)
	}
	if got := percentile(xs, 95); got < 45 || got > 50 {
		t.Fatalf("p95=%v out of expected range", got)
	}
	if got := percentile(xs, 100); got != 50 {
		t.Fatalf("p100=%v want=50", got)
	}
}
