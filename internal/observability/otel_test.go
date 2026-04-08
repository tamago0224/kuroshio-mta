package observability

import (
	"context"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestInitOTELDisabled(t *testing.T) {
	shutdown, err := InitOTEL(context.Background(), config.Config{})
	if err != nil {
		t.Fatalf("InitOTEL() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func should not be nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestInitOTELEnabledRequiresEndpoint(t *testing.T) {
	_, err := InitOTEL(context.Background(), config.Config{
		OTELEnabled:          true,
		OTELServiceName:      "kuroshio-mta",
		OTELTraceSampleRatio: 1.0,
	})
	if err == nil {
		t.Fatal("expected error when otel endpoint is empty")
	}
}
