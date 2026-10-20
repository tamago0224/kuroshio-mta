package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
)

func TestDeliveryPauseAPI(t *testing.T) {
	t.Parallel()
	store, err := deliverypause.NewLocal(filepath.Join(t.TempDir(), "pauses.json"))
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(nil, nil, nil, "viewer-token:viewer,operator-token:operator")
	api.SetDeliveryPauses(store)
	expiry := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name, method, path, body, token string
		code                            int
	}{
		{"viewer pause", http.MethodPost, "/api/v1/delivery/pauses", `{}`, "viewer-token", 403},
		{"invalid", http.MethodPost, "/api/v1/delivery/pauses", `{"domain":"a.test"}`, "operator-token", 400},
		{"dry-run", http.MethodPost, "/api/v1/delivery/pauses", fmt.Sprintf(`{"domain":"A.TEST","reason":"maintenance","expires_at":%q,"dry_run":true}`, expiry), "operator-token", 200},
		{"pause", http.MethodPost, "/api/v1/delivery/pauses", fmt.Sprintf(`{"domain":"A.TEST","reason":"maintenance","expires_at":%q}`, expiry), "operator-token", 200},
		{"list", http.MethodGet, "/api/v1/delivery/pauses", "", "viewer-token", 200},
		{"viewer resume", http.MethodDelete, "/api/v1/delivery/pauses/a.test", "", "viewer-token", 403},
		{"resume dry-run", http.MethodDelete, "/api/v1/delivery/pauses/a.test?dry_run=1&reason=restored", "", "operator-token", 200},
		{"resume", http.MethodDelete, "/api/v1/delivery/pauses/a.test?reason=restored", "", "operator-token", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			api.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			hold, err := store.Lookup(context.Background(), "a.test")
			if err != nil {
				t.Fatal(err)
			}
			if (tc.name == "dry-run" || tc.name == "resume") && hold.Domain != "" {
				t.Fatal("unexpected active hold")
			}
			if tc.name == "resume dry-run" && hold.Domain == "" {
				t.Fatal("dry-run resumed")
			}
			if tc.name == "list" && !strings.Contains(rec.Body.String(), "config:operator") {
				t.Fatal("trusted actor not stored")
			}
		})
	}
}
