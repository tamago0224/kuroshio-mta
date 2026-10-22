package smtp

import (
	"context"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

func TestEnqueueUsesOnlyAuthenticatedPrincipalForTenant(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		authenticated         bool
		wantTenant, wantClass string
	}{
		{name: "authenticated submission", authenticated: true, wantTenant: "alice@example.test", wantClass: "transactional"},
		{name: "unauthenticated inbound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &recordingQueue{}
			s := &Server{queue: q}
			ss := &session{authOK: tc.authenticated, auth: userauth.Principal{Username: "Alice@Example.Test"}, mailFrom: "sender@example.test", rcptTo: []string{"user@recipient.test"}}
			if err := s.enqueue(context.Background(), ss, "message-id"); err != nil {
				t.Fatal(err)
			}
			if got := q.msgs[0]; got.Tenant != tc.wantTenant || got.TrafficClass != tc.wantClass {
				t.Fatalf("queue identity = %q/%q", got.Tenant, got.TrafficClass)
			}
		})
	}
}
