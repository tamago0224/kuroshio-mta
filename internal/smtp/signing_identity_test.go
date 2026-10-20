package smtp

import (
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

func TestSigningIdentityRequiresAuthenticatedSenderScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mailFrom, headers string
		principal               userauth.Principal
		authenticated           bool
		want                    string
		fail                    bool
	}{
		{name: "authenticated", mailFrom: "alice@sender.test", headers: "From: Alice <alice@sender.test>\r\n", principal: userauth.Principal{Username: "alice@sender.test"}, authenticated: true, want: "sender.test"},
		{name: "untrusted spoof header", mailFrom: "alice@sender.test", headers: "From: alice@sender.test\r\nX-Kuroshio-Signing-Domain: sender.test\r\n"},
		{name: "outside envelope scope", mailFrom: "alice@other.test", headers: "From: alice@other.test\r\n", principal: userauth.Principal{Username: "alice@sender.test"}, authenticated: true, fail: true},
		{name: "outside author scope", mailFrom: "alice@sender.test", headers: "From: bob@other.test\r\n", principal: userauth.Principal{Username: "alice@sender.test"}, authenticated: true, fail: true},
		{name: "multiple authors", mailFrom: "alice@sender.test", headers: "From: alice@sender.test, bob@sender.test\r\n", principal: userauth.Principal{Username: "alice@sender.test"}, authenticated: true, fail: true},
		{name: "multiple From fields", mailFrom: "alice@sender.test", headers: "From: alice@sender.test\r\nFrom: bob@sender.test\r\n", principal: userauth.Principal{Username: "alice@sender.test"}, authenticated: true, fail: true},
		{name: "explicit address scope", mailFrom: "bob@sender.test", headers: "From: bob@sender.test\r\n", principal: userauth.Principal{Username: "alice@sender.test", RequireExplicitSenderScope: true, AllowedSenderAddresses: []string{"alice@sender.test"}}, authenticated: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: config.Config{DKIMProfilesFile: "/admin/profiles.json"}, submission: true}
			identity, err := s.signingIdentity(&session{authOK: tc.authenticated, auth: tc.principal, mailFrom: tc.mailFrom, data: []byte(tc.headers + "Subject: test\r\n\r\nbody")})
			if (err != nil) != tc.fail {
				t.Fatalf("authorization: %v", err)
			}
			if !tc.fail && (identity == nil || identity.Domain != tc.want) {
				t.Fatalf("identity: %+v", identity)
			}
		})
	}
}

func TestSigningIdentityPersistsPastSessionReset(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	s := &Server{queue: q}
	ss := &session{mailFrom: "alice@sender.test", signing: &model.SigningIdentity{Domain: "sender.test"}, data: []byte("From: alice@sender.test\r\n\r\nbody")}
	if err := s.enqueue(t.Context(), ss, "signed"); err != nil {
		t.Fatal(err)
	}
	ss.finishData()
	if len(q.msgs) != 1 || q.msgs[0].SigningIdentity == nil || q.msgs[0].SigningIdentity.Domain != "sender.test" || ss.signing != nil {
		t.Fatal("trusted identity was not retained independently of session")
	}
}
