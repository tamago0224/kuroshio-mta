package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTLSReportConfigYAML(t *testing.T) {
	t.Parallel()
	var cfg TLSReportConfig
	err := yaml.Unmarshal([]byte("enabled: true\norganization: Kuroshio\ncontact_info: postmaster@example.test\nreport_domain: sender.test\nflush_interval: 2h\nmax_records: 20\nmax_report_bytes: 2048\n"), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FlushInterval != 2*time.Hour || cfg.MaxRecords != 20 || cfg.MaxReportBytes != 2048 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
