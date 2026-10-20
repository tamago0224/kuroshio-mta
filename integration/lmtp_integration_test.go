//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

func TestLMTPMailstore(t *testing.T) {
	socket := os.Getenv("MTA_TEST_LMTP_UNIX_SOCKET")
	recipient := os.Getenv("MTA_TEST_LMTP_RECIPIENT")
	if socket == "" || recipient == "" {
		t.Skip("MTA_TEST_LMTP_UNIX_SOCKET and MTA_TEST_LMTP_RECIPIENT are required")
	}
	domain, ok := util.DomainOf(recipient)
	if !ok {
		t.Fatal("MTA_TEST_LMTP_RECIPIENT must contain a valid domain")
	}
	cfg := config.Config{
		Hostname:    "integration.kuroshio.test",
		DialTimeout: 3 * time.Second,
		SendTimeout: 10 * time.Second,
		DeliveryRoutes: []config.DeliveryRoute{
			{Name: "mailstore", Domains: []string{domain}, Mode: "lmtp", LMTPUnixSocket: socket},
		},
	}
	client := delivery.NewClient(cfg)
	defer client.Close()
	msg := &model.Message{ID: "lmtp-integration", MailFrom: "sender@kuroshio.test", Data: []byte("From: sender@kuroshio.test\r\nSubject: LMTP integration\r\n\r\nLMTP integration test")}
	results := client.DeliverBatch(context.Background(), msg, []string{recipient})
	if err := results[recipient]; err != nil {
		t.Fatal(err)
	}
}
