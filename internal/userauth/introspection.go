package userauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"
)

// IntrospectionConfig describes a trusted RFC 7662 authorization server.
// Issuer, Audience, expiry and RequiredScope are mandatory local policy.
type IntrospectionConfig struct {
	URL, ClientID, ClientSecret, Issuer, Audience, UsernameClaim, RequiredScope string
	HTTPClient                                                                  *http.Client
}

type IntrospectionBackend struct {
	cfg    IntrospectionConfig
	client *http.Client
}

func NewIntrospection(cfg IntrospectionConfig) (*IntrospectionBackend, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("invalid bearer introspection configuration")
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "sub"
	}
	if cfg.RequiredScope == "" {
		cfg.RequiredScope = "smtp.send"
	}
	if len(strings.Fields(cfg.RequiredScope)) != 1 {
		return nil, errors.New("bearer scope must be one nonempty scope")
	}
	client := http.Client{}
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	}
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &IntrospectionBackend{cfg: cfg, client: &client}, nil
}

func (b *IntrospectionBackend) AuthenticateBearerToken(ctx context.Context, user, token string) AuthResult {
	result := AuthResult{Principal: Principal{AuthSource: "oauth_introspection", Username: normalizeUsername(user)}, FailureReason: "invalid_token"}
	address, err := mail.ParseAddress(user)
	if err != nil || address.Address != user || !strings.Contains(user, "@") || token == "" || len(token) > 8192 {
		return result
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.URL, strings.NewReader(form.Encode()))
	if err != nil {
		result.FailureReason = "backend_unavailable"
		return result
	}
	// OAuth client credentials use form encoding before HTTP Basic encoding.
	req.SetBasicAuth(url.QueryEscape(b.cfg.ClientID), url.QueryEscape(b.cfg.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		result.FailureReason = "backend_unavailable"
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		result.FailureReason = "backend_unavailable"
		return result
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return result
	}
	var claims struct {
		Active    bool            `json:"active"`
		Exp       int64           `json:"exp"`
		Nbf       int64           `json:"nbf"`
		Iss       string          `json:"iss"`
		Sub       string          `json:"sub"`
		Aud       json.RawMessage `json:"aud"`
		Scope     string          `json:"scope"`
		TokenType string          `json:"token_type"`
	}
	if json.Unmarshal(raw, &claims) != nil || !claims.Active || claims.Exp <= time.Now().Unix() || claims.Nbf > time.Now().Unix() || claims.Iss != b.cfg.Issuer || claims.Sub == "" {
		return result
	}
	if claims.TokenType != "" && !strings.EqualFold(claims.TokenType, "Bearer") {
		return result
	}
	var audiences []string
	var audience string
	if json.Unmarshal(claims.Aud, &audience) == nil {
		audiences = []string{audience}
	} else if json.Unmarshal(claims.Aud, &audiences) != nil {
		return result
	}
	if !slices.Contains(audiences, b.cfg.Audience) || !slices.Contains(strings.Fields(claims.Scope), b.cfg.RequiredScope) {
		return result
	}
	var values map[string]json.RawMessage
	var mailbox string
	if json.Unmarshal(raw, &values) != nil || json.Unmarshal(values[b.cfg.UsernameClaim], &mailbox) != nil || normalizeUsername(mailbox) != result.Principal.Username {
		return result
	}
	result.Principal.RequireExplicitSenderScope = true
	result.Principal.AllowedSenderAddresses = []string{result.Principal.Username}
	result.Success = true
	result.FailureReason = ""
	return result
}
