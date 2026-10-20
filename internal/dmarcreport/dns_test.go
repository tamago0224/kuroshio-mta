package dmarcreport

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestAuthorizeExternalDestination(t *testing.T) {
	var queried string
	lookup := func(_ context.Context, name string) ([]string, error) {
		queried = name
		return []string{"v=DMARC1"}, nil
	}
	allowed, err := AuthorizeExternalDestination(context.Background(), "mail.example.test", "example.test", "reports.vendor.test", "vendor.test", lookup)
	if err != nil || !allowed || queried != "mail.example.test._report._dmarc.reports.vendor.test" {
		t.Fatalf("allowed=%v query=%q err=%v", allowed, queried, err)
	}
	queried = ""
	allowed, err = AuthorizeExternalDestination(context.Background(), "mail.example.test", "example.test", "reports.example.test", "example.test", lookup)
	if err != nil || !allowed || queried != "" {
		t.Fatalf("same org must not query; allowed=%v query=%q err=%v", allowed, queried, err)
	}
}

func TestAuthorizeExternalDestinationAllowsUnknownExtension(t *testing.T) {
	allowed, err := AuthorizeExternalDestination(context.Background(), "example.test", "example.test", "reports.vendor.test", "vendor.test", func(context.Context, string) ([]string, error) {
		return []string{"v=DMARC1; x-vendor=opaque"}, nil
	})
	if err != nil || !allowed {
		t.Fatalf("unknown authorization extension rejected: allowed=%v err=%v", allowed, err)
	}
}

func TestAuthorizeExternalDestinationFailsClosed(t *testing.T) {
	for _, record := range []string{"p=none; v=DMARC1", "v=DMARC2", "v=DMARC1; v=DMARC1", "v=DMARC1; broken"} {
		allowed, err := AuthorizeExternalDestination(context.Background(), "example.test", "example.test", "reports.vendor.test", "vendor.test", func(context.Context, string) ([]string, error) { return []string{record}, nil })
		if err != nil || allowed {
			t.Errorf("record %q allowed=%v err=%v", record, allowed, err)
		}
	}
	allowed, err := AuthorizeExternalDestination(context.Background(), "example.test", "example.test", "reports.vendor.test", "vendor.test", func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "timeout", IsTimeout: true}
	})
	if allowed || err == nil {
		t.Fatalf("DNS timeout allowed=%v err=%v", allowed, err)
	}
	allowed, err = AuthorizeExternalDestination(context.Background(), "example.test", "example.test", "reports.vendor.test", "vendor.test", func(context.Context, string) ([]string, error) { return nil, errors.New("network failure") })
	if allowed || err == nil {
		t.Fatalf("lookup failure allowed=%v err=%v", allowed, err)
	}
}
