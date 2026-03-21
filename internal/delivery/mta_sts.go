package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxMTASTSPolicyAge = 31557600 * time.Second

type MTASTSPolicy struct {
	Version   string
	Mode      string
	MX        []string
	MaxAge    time.Duration
	ExpiresAt time.Time
	PolicyID  string
}

func (p MTASTSPolicy) AllowsMX(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, pat := range p.MX {
		pat = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pat), "."))
		if pat == "" {
			continue
		}
		if strings.HasPrefix(pat, "*.") {
			suffix := strings.TrimPrefix(pat, "*.")
			if parts := strings.SplitN(host, ".", 2); len(parts) == 2 && parts[1] == suffix {
				return true
			}
			continue
		}
		if host == pat {
			return true
		}
	}
	return false
}

type MTASTSResolver struct {
	ttl       time.Duration
	fetchTO   time.Duration
	fetchFunc func(ctx context.Context, domain string) (string, error)
	lookupTXT func(ctx context.Context, name string) ([]string, error)
	nowFn     func() time.Time

	mu            sync.Mutex
	cache         map[string]MTASTSPolicy
	retryFailures map[string]int
	retryAt       map[string]time.Time
	minRetryDelay time.Duration
	maxRetryDelay time.Duration
	rollovers     map[string]mtaSTSRolloverState
	rolloverNeed  int
}

type mtaSTSRolloverState struct {
	policy        MTASTSPolicy
	confirmations int
}

func NewMTASTSResolver(ttl, fetchTimeout time.Duration, fetchFunc func(ctx context.Context, domain string) (string, error)) *MTASTSResolver {
	if fetchFunc == nil {
		fetchFunc = fetchMTASTSPolicyTextHTTP(fetchTimeout)
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if fetchTimeout <= 0 {
		fetchTimeout = 5 * time.Second
	}
	return &MTASTSResolver{
		ttl:           ttl,
		fetchTO:       fetchTimeout,
		fetchFunc:     fetchFunc,
		lookupTXT:     net.DefaultResolver.LookupTXT,
		nowFn:         time.Now,
		cache:         map[string]MTASTSPolicy{},
		retryFailures: map[string]int{},
		retryAt:       map[string]time.Time{},
		minRetryDelay: time.Second,
		maxRetryDelay: 5 * time.Minute,
		rollovers:     map[string]mtaSTSRolloverState{},
		rolloverNeed:  2,
	}
}

func (r *MTASTSResolver) Lookup(ctx context.Context, domain string) (MTASTSPolicy, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return MTASTSPolicy{}, errors.New("empty domain")
	}
	now := r.nowFn().UTC()
	policyID, hasPolicyID := r.lookupPolicyID(ctx, domain)

	var stale MTASTSPolicy
	hasStale := false

	r.mu.Lock()
	if p, ok := r.cache[domain]; ok {
		refreshByID := hasPolicyID && p.PolicyID != "" && policyID != p.PolicyID
		if now.Before(p.ExpiresAt) && !refreshByID {
			r.mu.Unlock()
			return p, nil
		}
		stale = p
		hasStale = true
		if !refreshByID {
			if ra, ok := r.retryAt[domain]; ok && now.Before(ra) {
				r.mu.Unlock()
				return stale, nil
			}
		} else {
			delete(r.retryAt, domain)
			delete(r.retryFailures, domain)
		}
	}
	r.mu.Unlock()

	text, err := r.fetchFunc(ctx, domain)
	if err != nil {
		if hasStale {
			r.mu.Lock()
			failures := r.retryFailures[domain] + 1
			r.retryFailures[domain] = failures
			r.retryAt[domain] = now.Add(r.retryDelay(failures))
			r.mu.Unlock()
			return stale, nil
		}
		return MTASTSPolicy{}, err
	}
	p, err := parseMTASTSPolicy(text)
	if err != nil {
		if hasStale {
			r.mu.Lock()
			failures := r.retryFailures[domain] + 1
			r.retryFailures[domain] = failures
			r.retryAt[domain] = now.Add(r.retryDelay(failures))
			r.mu.Unlock()
			return stale, nil
		}
		return MTASTSPolicy{}, err
	}
	expire := now.Add(p.MaxAge)
	if ttlExpire := now.Add(r.ttl); ttlExpire.Before(expire) {
		expire = ttlExpire
	}
	p.ExpiresAt = expire
	if hasPolicyID {
		p.PolicyID = policyID
	}

	r.mu.Lock()
	delete(r.retryFailures, domain)
	delete(r.retryAt, domain)
	if hasStale && stale.PolicyID != "" && p.PolicyID != "" && p.PolicyID != stale.PolicyID {
		if !r.observeRolloverLocked(domain, p) {
			r.mu.Unlock()
			return stale, nil
		}
	} else {
		delete(r.rollovers, domain)
	}
	r.cache[domain] = p
	delete(r.rollovers, domain)
	r.mu.Unlock()
	return p, nil
}

func (r *MTASTSResolver) observeRolloverLocked(domain string, p MTASTSPolicy) bool {
	if r.rolloverNeed <= 1 {
		return true
	}
	st, ok := r.rollovers[domain]
	if !ok || st.policy.PolicyID != p.PolicyID {
		r.rollovers[domain] = mtaSTSRolloverState{
			policy:        p,
			confirmations: 1,
		}
		return false
	}
	st.confirmations++
	st.policy = p
	if st.confirmations >= r.rolloverNeed {
		return true
	}
	r.rollovers[domain] = st
	return false
}

func (r *MTASTSResolver) lookupPolicyID(ctx context.Context, domain string) (string, bool) {
	if r.lookupTXT == nil {
		return "", false
	}
	name := "_mta-sts." + domain
	txts, err := r.lookupTXT(ctx, name)
	if err != nil {
		return "", false
	}
	return parseMTASTSPolicyID(txts)
}

func parseMTASTSPolicyID(txts []string) (string, bool) {
	for _, txt := range txts {
		var versionOK bool
		var policyID string
		parts := strings.Split(txt, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(kv[0]))
			v := strings.TrimSpace(kv[1])
			switch k {
			case "v":
				if strings.EqualFold(v, "STSv1") {
					versionOK = true
				}
			case "id":
				if v != "" {
					policyID = v
				}
			}
		}
		if versionOK && policyID != "" {
			return policyID, true
		}
	}
	return "", false
}

func (r *MTASTSResolver) retryDelay(failures int) time.Duration {
	if failures <= 0 {
		return r.minRetryDelay
	}
	delay := r.minRetryDelay
	for i := 1; i < failures; i++ {
		if delay >= r.maxRetryDelay/2 {
			delay = r.maxRetryDelay
			break
		}
		delay *= 2
	}
	if delay > r.maxRetryDelay {
		return r.maxRetryDelay
	}
	if delay <= 0 {
		return r.minRetryDelay
	}
	return delay
}

func parseMTASTSPolicy(raw string) (MTASTSPolicy, error) {
	var p MTASTSPolicy
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.TrimSpace(parts[1])
		switch k {
		case "version":
			p.Version = v
		case "mode":
			p.Mode = strings.ToLower(v)
		case "mx":
			if v != "" {
				p.MX = append(p.MX, v)
			}
		case "max_age":
			n, err := strconv.Atoi(v)
			if err != nil {
				return MTASTSPolicy{}, fmt.Errorf("invalid max_age: %w", err)
			}
			p.MaxAge = time.Duration(n) * time.Second
		}
	}
	if p.Version != "STSv1" {
		return MTASTSPolicy{}, errors.New("invalid mta-sts version")
	}
	switch p.Mode {
	case "enforce", "testing", "none":
	default:
		return MTASTSPolicy{}, errors.New("invalid mta-sts mode")
	}
	if p.MaxAge <= 0 {
		return MTASTSPolicy{}, errors.New("max_age must be positive")
	}
	if p.MaxAge > maxMTASTSPolicyAge {
		return MTASTSPolicy{}, errors.New("max_age exceeds RFC 8461 maximum")
	}
	if p.Mode != "none" && len(p.MX) == 0 {
		return MTASTSPolicy{}, errors.New("mx is required for non-none mode")
	}
	return p, nil
}

func fetchMTASTSPolicyTextHTTP(timeout time.Duration) func(context.Context, string) (string, error) {
	client := newMTASTSHTTPClient(timeout)
	return func(ctx context.Context, domain string) (string, error) {
		url := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			if isMTASTSCertificateValidationError(err) {
				return "", fmt.Errorf("mta-sts https certificate validation failed: %w", err)
			}
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
			mediaType, params, err := mime.ParseMediaType(ct)
			if err != nil {
				return "", fmt.Errorf("invalid content-type: %w", err)
			}
			if !isAllowedMTASTSPolicyMediaType(mediaType, params) {
				return "", fmt.Errorf("unexpected content-type %q", ct)
			}
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func isAllowedMTASTSPolicyMediaType(mediaType string, _ map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(mediaType), "text/plain")
}

func newMTASTSHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirect is not allowed for mta-sts policy fetch")
		},
	}
}

func isMTASTSCertificateValidationError(err error) bool {
	if err == nil {
		return false
	}
	var uErr *url.Error
	if errors.As(err, &uErr) {
		err = uErr.Err
	}
	var uaErr *x509.UnknownAuthorityError
	if errors.As(err, &uaErr) {
		return true
	}
	var hnErr *x509.HostnameError
	if errors.As(err, &hnErr) {
		return true
	}
	var ciErr *x509.CertificateInvalidError
	if errors.As(err, &ciErr) {
		return true
	}
	return false
}
