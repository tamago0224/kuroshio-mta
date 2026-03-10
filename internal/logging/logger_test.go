package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewJSONLoggerOutputsStructuredRecord(t *testing.T) {
	var buf bytes.Buffer
	l := New("debug", &buf)
	l.Info("smtp listening", slog.String("component", "smtp"), slog.String("remote_ip", "127.0.0.1"))
	out := buf.String()
	if !strings.Contains(out, `"msg":"smtp listening"`) {
		t.Fatalf("missing msg field: %q", out)
	}
	if !strings.Contains(out, `"component":"smtp"`) {
		t.Fatalf("missing component field: %q", out)
	}
	if !strings.Contains(out, `"remote_ip":"127.0.0.1"`) {
		t.Fatalf("missing remote_ip field: %q", out)
	}
}

func TestNewLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New("warn", &buf)
	l.Info("info should be filtered")
	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Fatalf("unexpected output at warn level: %q", got)
	}
}
