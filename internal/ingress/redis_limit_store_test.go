package ingress

import "testing"

func TestNewLimitStoreRejectsUnsupportedBackend(t *testing.T) {
	if _, err := NewLimitStore(RateLimitStoreConfig{Backend: "unknown"}); err == nil {
		t.Fatal("expected unsupported backend error")
	}
}

func TestNormalizeRedisAddrs(t *testing.T) {
	got := normalizeRedisAddrs([]string{" localhost:6379 ", "", " valkey:6379"})
	if len(got) != 2 {
		t.Fatalf("len(got)=%d", len(got))
	}
	if got[0] != "localhost:6379" || got[1] != "valkey:6379" {
		t.Fatalf("got=%v", got)
	}
}
