// Package policyhook implements kuroshio's own SMTP policy API, not MTA Hooks.
package policyhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

const Version = "kuroshio.smtp-policy.v1"

type Action string

const (
	Accept   Action = "accept"
	Reject   Action = "reject"
	Tempfail Action = "tempfail"
)

// Request intentionally contains no SMTP AUTH credentials or authenticated user
// identity. Message contains final DATA bytes and is opt-in, encoded as base64.
type Request struct {
	Version       string   `json:"version"`
	Stage         string   `json:"stage"`
	RemoteIP      string   `json:"remote_ip"`
	HELO          string   `json:"helo"`
	MailFrom      string   `json:"mail_from"`
	Recipients    []string `json:"recipients"`
	Recipient     string   `json:"recipient,omitempty"`
	TLS           bool     `json:"tls"`
	Submission    bool     `json:"submission"`
	Authenticated bool     `json:"authenticated"`
	MessageBytes  int      `json:"message_bytes"`
	Message       []byte   `json:"message_base64,omitempty"`
}
type Response struct {
	Version string `json:"version"`
	Action  Action `json:"action"`
}
type Evaluator interface {
	Evaluate(context.Context, Request) (Action, error)
}

type Client struct {
	cfg       config.PolicyHookConfig
	http      *http.Client
	transport *http.Transport
	slots     chan struct{}
	token     string
	metrics   *observability.Metrics
}

func New(cfg config.PolicyHookConfig, metrics *observability.Metrics) (*Client, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 32
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 64 * 1024
	}
	token := ""
	if cfg.TokenFile != "" {
		file, err := os.Open(cfg.TokenFile)
		if err != nil {
			return nil, errors.New("policy hook token file cannot be opened")
		}
		data, err := io.ReadAll(io.LimitReader(file, 4097))
		file.Close()
		if err != nil || len(data) > 4096 {
			return nil, errors.New("policy hook token file unreadable or too large")
		}
		token = strings.TrimSpace(string(data))
		if token == "" || strings.ContainsAny(token, "\r\n\x00") {
			return nil, errors.New("policy hook token must be a nonempty single line")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Administrator endpoint only; no environment-selected proxy.
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DisableCompression = true
	transport.MaxResponseHeaderBytes = 64 * 1024
	transport.MaxConnsPerHost = cfg.MaxConcurrent
	transport.MaxIdleConnsPerHost = cfg.MaxConcurrent
	return &Client{cfg: cfg, token: token, transport: transport, http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, slots: make(chan struct{}, cfg.MaxConcurrent), metrics: metrics}, nil
}
func (c *Client) Close() {
	if c != nil {
		c.transport.CloseIdleConnections()
	}
}
func (c *Client) count(stage, result string) {
	if c.metrics != nil {
		c.metrics.Counter("smtp_policy_hook_" + stage + "_" + result).Inc()
	}
}
func (c *Client) fail(stage, reason string) (Action, error) {
	c.count(stage, "tempfail")
	c.count(stage, "error_"+reason)
	return Tempfail, errors.New("policy hook " + reason)
}

func (c *Client) Evaluate(ctx context.Context, request Request) (Action, error) {
	if request.Stage != "rcpt" && request.Stage != "data" {
		return Tempfail, errors.New("unsupported policy hook stage")
	}
	stage := request.Stage
	if err := ctx.Err(); err != nil {
		return c.fail(stage, "canceled")
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return c.fail(stage, "overload")
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	request.Version = Version
	if !c.cfg.SendBody || stage != "data" {
		request.Message = nil
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return c.fail(stage, "request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return c.fail(stage, "request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		reason := "transport"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			reason = "canceled"
		}
		return c.fail(stage, reason)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return c.fail(stage, "http_status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, c.cfg.MaxResponseBytes+1))
	if err != nil {
		reason := "read"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		return c.fail(stage, reason)
	}
	if int64(len(body)) > c.cfg.MaxResponseBytes {
		return c.fail(stage, "response_too_large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var decision Response
	if err := decoder.Decode(&decision); err != nil {
		return c.fail(stage, "json")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return c.fail(stage, "json")
	}
	if decision.Version != Version {
		return c.fail(stage, "version")
	}
	switch decision.Action {
	case Accept, Reject, Tempfail:
		c.count(stage, string(decision.Action))
		return decision.Action, nil
	default:
		return c.fail(stage, "action")
	}
}
