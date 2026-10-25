package smtp

import (
	"bufio"
	"strings"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestParseMailFromRequireTLS(t *testing.T) {
	got, err := parseMailFrom("FROM:<sender@example.test> REQUIRETLS")
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequireTLS {
		t.Fatal("REQUIRETLS was not parsed")
	}
	if _, err := parseMailFrom("FROM:<sender@example.test> REQUIRETLS REQUIRETLS"); err == nil {
		t.Fatal("duplicate REQUIRETLS was accepted")
	}
}

func TestRequireTLSAvailabilityIsFailClosed(t *testing.T) {
	tests := []struct {
		name       string
		submission bool
		cfg        config.Config
		want       bool
	}{
		{name: "authenticated submission with MX", submission: true, cfg: config.Config{SubmissionAuth: true, DeliveryMode: "mx"}, want: true},
		{name: "inbound MX", cfg: config.Config{SubmissionAuth: true, DeliveryMode: "mx"}},
		{name: "submission without auth", submission: true, cfg: config.Config{DeliveryMode: "mx"}},
		{name: "local spool", submission: true, cfg: config.Config{SubmissionAuth: true, DeliveryMode: "local_spool"}},
		{name: "LMTP route", submission: true, cfg: config.Config{SubmissionAuth: true, DeliveryMode: "mx", DeliveryRoutes: []config.DeliveryRoute{{Mode: "lmtp"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{submission: tt.submission, cfg: tt.cfg}
			if got := s.requireTLSAvailable(); got != tt.want {
				t.Fatalf("requireTLSAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEHLOAdvertisesRequireTLSOnlyWhenAvailable(t *testing.T) {
	var output strings.Builder
	w := bufio.NewWriter(&output)
	writeEHLOResponse(w, "mx.test", 1024, true, true, true, "PLAIN")
	if !strings.Contains(output.String(), "250-REQUIRETLS\r\n") {
		t.Fatalf("EHLO = %q", output.String())
	}
}
