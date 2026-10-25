package httpinjection

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

type memoryQueue struct {
	mu       sync.Mutex
	messages []*model.Message
}

func (q *memoryQueue) Enqueue(msg *model.Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, msg)
	return nil
}
func (*memoryQueue) Due(int) ([]*model.Message, error)                 { return nil, nil }
func (*memoryQueue) AckSent(string, *model.Message) error              { return nil }
func (*memoryQueue) Retry(*model.Message, time.Duration, string) error { return nil }
func (*memoryQueue) Defer(*model.Message, time.Duration) error         { return nil }
func (*memoryQueue) Fail(*model.Message, string) error                 { return nil }
func (*memoryQueue) Close() error                                      { return nil }

func TestAPIAuthenticationValidationAndIdempotency(t *testing.T) {
	api, backend := newTestAPI(t)
	tests := []struct {
		name       string
		key        string
		from       string
		auth       bool
		wantStatus int
	}{
		{name: "unauthenticated", key: "unauth", from: "alice@example.test", wantStatus: http.StatusUnauthorized},
		{name: "sender outside scope", key: "bad-sender", from: "alice@other.test", auth: true, wantStatus: http.StatusUnprocessableEntity},
		{name: "accepted", key: "accepted", from: "alice@example.test", auth: true, wantStatus: http.StatusAccepted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := newInjectRequest(t, tc.key, tc.from, "Subject: test\nFrom: alice@example.test\n\nhello\n")
			if tc.auth {
				request.SetBasicAuth("alice@example.test", "secret")
			}
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	first := newInjectRequest(t, "same-key", "alice@example.test", "From: alice@example.test\nSubject: same\n\nhello")
	first.SetBasicAuth("alice@example.test", "secret")
	firstResponse := httptest.NewRecorder()
	api.Handler().ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	replay := newInjectRequest(t, "same-key", "alice@example.test", "From: alice@example.test\nSubject: same\n\nhello")
	replay.SetBasicAuth("alice@example.test", "secret")
	replayResponse := httptest.NewRecorder()
	api.Handler().ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK || !bytes.Contains(replayResponse.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
	conflict := newInjectRequest(t, "same-key", "alice@example.test", "From: alice@example.test\nSubject: changed\n\nhello")
	conflict.SetBasicAuth("alice@example.test", "secret")
	conflictResponse := httptest.NewRecorder()
	api.Handler().ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.messages) != 2 {
		t.Fatalf("queued=%d want=2", len(backend.messages))
	}
	msg := backend.messages[1]
	if msg.Tenant != "alice@example.test" || msg.SigningIdentity == nil || msg.SigningIdentity.Domain != "example.test" {
		t.Fatalf("unexpected trusted metadata: %+v", msg)
	}
	withoutCRLF := bytes.ReplaceAll(msg.Data, []byte("\r\n"), nil)
	if !bytes.Contains(msg.Data, []byte("\r\n\r\n")) || bytes.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatalf("message was not normalized to CRLF: %q", msg.Data)
	}
}

func TestAPIConcurrentReplayAndRestart(t *testing.T) {
	api, backend := newTestAPI(t)
	const requests = 12
	var wg sync.WaitGroup
	statuses := make(chan int, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := newInjectRequest(t, "concurrent-key", "alice@example.test", "From: alice@example.test\n\nhello")
			request.SetBasicAuth("alice@example.test", "secret")
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			statuses <- response.Code
		}()
	}
	wg.Wait()
	close(statuses)
	accepted := 0
	for status := range statuses {
		if status == http.StatusAccepted {
			accepted++
		} else if status != http.StatusOK {
			t.Fatalf("unexpected status %d", status)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted=%d want=1", accepted)
	}
	backend.mu.Lock()
	if len(backend.messages) != 1 {
		t.Fatalf("queued=%d want=1", len(backend.messages))
	}
	backend.mu.Unlock()

	restarted, err := New(api.cfg, backend, api.password, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := newInjectRequest(t, "concurrent-key", "alice@example.test", "From: alice@example.test\n\nhello")
	request.SetBasicAuth("alice@example.test", "secret")
	response := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("restart replay status=%d body=%s", response.Code, response.Body.String())
	}
}

func newTestAPI(t *testing.T) (*API, *memoryQueue) {
	t.Helper()
	auth, err := userauth.NewStatic("alice@example.test:secret")
	if err != nil {
		t.Fatal(err)
	}
	backend := &memoryQueue{}
	cfg := config.Config{
		QueueDir:           t.TempDir(),
		MaxMessageBytes:    1024 * 1024,
		SubmissionSenderID: true,
		SMTPLimits:         config.SMTPLimitsConfig{MaxRecipients: 10},
		HTTPInjection: config.HTTPInjectionConfig{
			IdempotencyTTL:     time.Hour,
			RateLimitPerMinute: 100,
		},
	}
	api, err := New(cfg, backend, auth, nil)
	if err != nil {
		t.Fatal(err)
	}
	return api, backend
}

func newInjectRequest(t *testing.T, key, from, raw string) *http.Request {
	t.Helper()
	payload, err := json.Marshal(injectRequest{
		EnvelopeFrom: from,
		Recipients:   []string{"bob@recipient.test"},
		RawMessage:   raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	return request
}
