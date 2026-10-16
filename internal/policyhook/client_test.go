package policyhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestResponsesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       Action
		fail       bool
	}{
		{"accept", `{"version":"kuroshio.smtp-policy.v1","action":"accept"}`, 200, Accept, false},
		{"reject", `{"version":"kuroshio.smtp-policy.v1","action":"reject"}`, 200, Reject, false},
		{"tempfail", `{"version":"kuroshio.smtp-policy.v1","action":"tempfail"}`, 200, Tempfail, false},
		{"bad JSON", `{`, 200, Tempfail, true},
		{"trailing JSON", `{"version":"kuroshio.smtp-policy.v1","action":"accept"}{}`, 200, Tempfail, true},
		{"unknown version", `{"version":"other","action":"accept"}`, 200, Tempfail, true},
		{"unknown action", `{"version":"kuroshio.smtp-policy.v1","action":"rewrite"}`, 200, Tempfail, true},
		{"mutation", `{"version":"kuroshio.smtp-policy.v1","action":"accept","body":"changed"}`, 200, Tempfail, true},
		{"oversized", strings.Repeat("x", 1025), 200, Tempfail, true},
		{"http failure", `{}`, 500, Tempfail, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			client, err := New(config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true, MaxResponseBytes: 1024}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			action, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"})
			if action != tc.want || (err != nil) != tc.fail {
				t.Fatalf("decision %s, error %v", action, err)
			}
		})
	}
}

func TestRequestPrivacyAndBodyOptIn(t *testing.T) {
	for _, send := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata only", true: "body enabled"}[send], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Version != Version || request.Stage != "data" {
					t.Error("missing schema version/stage")
				}
				if (len(request.Message) > 0) != send {
					t.Error("body opt-in violated")
				}
				w.Write([]byte(`{"version":"kuroshio.smtp-policy.v1","action":"accept"}`))
			}))
			defer server.Close()
			client, err := New(config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true, SendBody: send}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if _, err := client.Evaluate(context.Background(), Request{Stage: "data", Message: []byte("private message")}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTimeoutAndOverload(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte(`{"version":"kuroshio.smtp-policy.v1","action":"accept"}`))
	}))
	defer server.Close()
	client, err := New(config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true, MaxConcurrent: 1, Timeout: 200 * time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() { _, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"}); done <- err }()
	<-entered
	if action, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"}); action != Tempfail || err == nil {
		t.Fatal("overload did not tempfail")
	}
	if err := <-done; err == nil {
		t.Fatal("timeout ignored")
	}
	close(release)
}

func TestTLSRedirectAndTokenPolicy(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("private-hook-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-hook-token" {
			t.Error("token missing")
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := New(config.PolicyHookConfig{URL: origin.URL, TokenFile: tokenFile}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if action, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"}); action != Tempfail || err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	// Positive trust is injected only into the test transport. Production uses OS roots.
	client.transport.TLSClientConfig.RootCAs = origin.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	action, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"})
	if action != Tempfail || err == nil || forwarded.Load() != 0 {
		t.Fatal("redirect forwarded private request/token")
	}
	if strings.Contains(err.Error(), "private-hook-token") || strings.Contains(err.Error(), origin.URL) {
		t.Fatal("hook error leaked secret or URL")
	}
}

func TestConnectionFailureAndCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client, err := New(config.PolicyHookConfig{URL: server.URL, AllowHTTPLoopback: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if action, err := client.Evaluate(context.Background(), Request{Stage: "rcpt"}); action != Tempfail || err == nil {
		t.Fatal("connection failure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if action, err := client.Evaluate(ctx, Request{Stage: "rcpt"}); action != Tempfail || err == nil {
		t.Fatal("cancellation ignored")
	}
}
