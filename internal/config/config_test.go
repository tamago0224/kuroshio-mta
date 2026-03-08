package config

import (
	"os"
	"testing"
	"time"
)

func TestEnvDurationList(t *testing.T) {
	def := []time.Duration{time.Minute, 2 * time.Minute}

	t.Setenv("MTA_RETRY_SCHEDULE", "5m,30m,2h")
	got := envDurationList("MTA_RETRY_SCHEDULE", def)
	want := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d len(want)=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%s want=%s", i, got[i], want[i])
		}
	}

	t.Setenv("MTA_RETRY_SCHEDULE", "bad")
	got = envDurationList("MTA_RETRY_SCHEDULE", def)
	if len(got) != len(def) || got[0] != def[0] || got[1] != def[1] {
		t.Fatalf("invalid value should fall back to default: got=%v def=%v", got, def)
	}

	_ = os.Unsetenv("MTA_RETRY_SCHEDULE")
}
