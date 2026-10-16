package userauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntrospectionPolicy(t *testing.T) {
	cases := []struct {
		name    string
		edit    func(map[string]any)
		success bool
	}{
		{"valid", func(m map[string]any) {}, true},
		{"array audience", func(m map[string]any) { m["aud"] = []string{"other", "smtp"} }, true},
		{"revoked", func(m map[string]any) { m["active"] = false }, false},
		{"expired", func(m map[string]any) { m["exp"] = time.Now().Unix() - 1 }, false},
		{"missing expiry", func(m map[string]any) { delete(m, "exp") }, false},
		{"future", func(m map[string]any) { m["nbf"] = time.Now().Unix() + 3600 }, false},
		{"issuer", func(m map[string]any) { m["iss"] = "evil" }, false},
		{"audience", func(m map[string]any) { m["aud"] = "other" }, false},
		{"scope", func(m map[string]any) { m["scope"] = "smtp.send.more" }, false},
		{"mailbox", func(m map[string]any) { m["email"] = "other@example.com" }, false},
		{"subject", func(m map[string]any) { delete(m, "sub") }, false},
		{"refresh token", func(m map[string]any) { m["token_type"] = "refresh_token" }, false},
		{"malformed expiry", func(m map[string]any) { m["exp"] = "tomorrow" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"active": true, "exp": time.Now().Unix() + 300, "iss": "issuer", "sub": "opaque-id", "email": "alice@example.com", "aud": "smtp", "scope": "openid smtp.send"}
			tc.edit(claims)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, secret, ok := r.BasicAuth()
				if !ok || id != "client" || secret != "secret" || r.Method != "POST" {
					t.Error("invalid authenticated request")
				}
				if r.ParseForm() != nil || r.Form.Get("token") != "test-access-token" || r.Form.Get("token_type_hint") != "access_token" {
					t.Error("invalid form")
				}
				_ = json.NewEncoder(w).Encode(claims)
			}))
			defer server.Close()
			b, err := NewIntrospection(IntrospectionConfig{URL: server.URL, ClientID: "client", ClientSecret: "secret", Issuer: "issuer", Audience: "smtp", UsernameClaim: "email", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			got := b.AuthenticateBearerToken(context.Background(), "alice@example.com", "test-access-token")
			if got.Success != tc.success {
				t.Fatalf("result=%+v", got)
			}
			if got.Success && (got.Principal.AuthSource != "oauth_introspection" || len(got.Principal.AllowedSenderAddresses) != 1 || got.Principal.AllowedSenderAddresses[0] != "alice@example.com") {
				t.Fatalf("principal=%+v", got.Principal)
			}
			if strings.Contains(got.FailureReason, "test-access-token") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestIntrospectionTransportFailures(t *testing.T) {
	for _, mode := range []string{"redirect", "status", "large", "malformed", "untrusted TLS", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					w.Header().Set("Location", "http://127.0.0.1/token-leak")
					w.WriteHeader(302)
				case "status":
					w.WriteHeader(503)
				case "large":
					_, _ = w.Write([]byte(strings.Repeat(" ", 65537)))
				default:
					_, _ = w.Write([]byte(`{"active":true}trailing`))
				}
			}))
			defer server.Close()
			cfg := IntrospectionConfig{URL: server.URL, ClientID: "client", ClientSecret: "secret", Issuer: "issuer", Audience: "smtp", HTTPClient: server.Client()}
			if mode == "untrusted TLS" {
				cfg.HTTPClient = nil
			}
			b, err := NewIntrospection(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "canceled" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if got := b.AuthenticateBearerToken(ctx, "alice@example.com", "token"); got.Success {
				t.Fatal("accepted failed transport")
			}
		})
	}
}

func TestIntrospectionConfiguration(t *testing.T) {
	valid := IntrospectionConfig{URL: "https://idp.example/introspect", ClientID: "id", ClientSecret: "secret", Issuer: "issuer", Audience: "smtp"}
	for _, endpoint := range []string{"http://idp.example", "https://user:pass@idp.example", "https://idp.example/#fragment", "https://idp.example/?token=secret", ":bad"} {
		cfg := valid
		cfg.URL = endpoint
		if _, err := NewIntrospection(cfg); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, field := range []string{"client", "secret", "issuer", "audience", "scope"} {
		cfg := valid
		switch field {
		case "client":
			cfg.ClientID = ""
		case "secret":
			cfg.ClientSecret = ""
		case "issuer":
			cfg.Issuer = ""
		case "audience":
			cfg.Audience = ""
		case "scope":
			cfg.RequiredScope = "one two"
		}
		if _, err := NewIntrospection(cfg); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
}

func TestCombinedMechanisms(t *testing.T) {
	password, _ := NewStatic("alice@example.com:secret")
	b := &CombinedBackend{Password: password}
	if strings.Join(b.AuthMechanisms(), " ") != "PLAIN LOGIN" {
		t.Fatal(b.AuthMechanisms())
	}
	if !b.AuthenticatePasswordDetailed("alice@example.com", "secret").Success {
		t.Fatal("password regression")
	}
	b.Password = nil
	if b.AuthenticatePasswordDetailed("alice@example.com", "secret").Success {
		t.Fatal("password unexpectedly enabled")
	}
}

func TestIntrospectionRevocationAndDefaultMailbox(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"active": calls == 1, "sub": "alice@example.com", "aud": "smtp", "iss": "issuer", "exp": time.Now().Unix() + 300, "scope": "smtp.send"})
	}))
	defer server.Close()
	b, err := NewIntrospection(IntrospectionConfig{URL: server.URL, ClientID: "id", ClientSecret: "secret", Issuer: "issuer", Audience: "smtp", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if !b.AuthenticateBearerToken(context.Background(), "alice@example.com", "token").Success {
		t.Fatal("default subject rejected")
	}
	if b.AuthenticateBearerToken(context.Background(), "alice@example.com", "token").Success {
		t.Fatal("revocation ignored")
	}
	if calls != 2 {
		t.Fatal("cached token validation")
	}
}
