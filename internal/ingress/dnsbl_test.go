package ingress

import (
	"errors"
	"testing"
	"time"
)

func TestDNSBLCheckerListedAndCache(t *testing.T) {
	calls := 0
	checker := NewDNSBLChecker([]string{"zen.example.org"}, 5*time.Minute, func(name string) ([]string, error) {
		calls++
		if name == "4.3.2.1.zen.example.org" {
			return []string{"127.0.0.2"}, nil
		}
		return nil, errors.New("not found")
	})

	listed, zone := checker.IsListed("1.2.3.4")
	if !listed || zone != "zen.example.org" {
		t.Fatalf("listed=%v zone=%q", listed, zone)
	}
	listed, zone = checker.IsListed("1.2.3.4")
	if !listed || zone != "zen.example.org" {
		t.Fatalf("cached listed=%v zone=%q", listed, zone)
	}
	if calls != 1 {
		t.Fatalf("expected single resolver call due to cache, calls=%d", calls)
	}
}

func TestDNSBLCheckerNotListed(t *testing.T) {
	checker := NewDNSBLChecker([]string{"zen.example.org"}, 5*time.Minute, func(name string) ([]string, error) {
		return nil, errors.New("not found")
	})
	listed, zone := checker.IsListed("1.2.3.4")
	if listed || zone != "" {
		t.Fatalf("listed=%v zone=%q", listed, zone)
	}
}
