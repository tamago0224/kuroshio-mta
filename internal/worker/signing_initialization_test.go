package worker

import (
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
)

func TestNewRejectsInvalidConfiguredSigner(t *testing.T) {
	t.Parallel()
	cfg := config.Config{DKIMSignDomain: "sender.test"}
	client := delivery.NewClient(cfg)
	defer client.Close()
	if _, err := New(cfg, nil, client, nil, nil, nil); err == nil {
		t.Fatal("worker started with unavailable configured signer")
	}
}
