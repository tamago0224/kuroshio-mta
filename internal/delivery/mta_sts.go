package delivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MTASTSPolicy struct {
	Version   string
	Mode      string
	MX        []string
	MaxAge    time.Duration
	ExpiresAt time.Time
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
			if strings.HasSuffix(host, "."+suffix) {
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

	mu    sync.Mutex
	cache map[string]MTASTSPolicy
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
	return &MTASTSResolver{ttl: ttl, fetchTO: fetchTimeout, fetchFunc: fetchFunc, cache: map[string]MTASTSPolicy{}}
}

func (r *MTASTSResolver) Lookup(ctx context.Context, domain string) (MTASTSPolicy, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return MTASTSPolicy{}, errors.New("empty domain")
	}
	now := time.Now().UTC()
	r.mu.Lock()
	if p, ok := r.cache[domain]; ok && now.Before(p.ExpiresAt) {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	text, err := r.fetchFunc(ctx, domain)
	if err != nil {
		return MTASTSPolicy{}, err
	}
	p, err := parseMTASTSPolicy(text)
	if err != nil {
		return MTASTSPolicy{}, err
	}
	expire := now.Add(p.MaxAge)
	if ttlExpire := now.Add(r.ttl); ttlExpire.Before(expire) {
		expire = ttlExpire
	}
	p.ExpiresAt = expire

	r.mu.Lock()
	r.cache[domain] = p
	r.mu.Unlock()
	return p, nil
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
	if p.Mode != "none" && len(p.MX) == 0 {
		return MTASTSPolicy{}, errors.New("mx is required for non-none mode")
	}
	return p, nil
}

func fetchMTASTSPolicyTextHTTP(timeout time.Duration) func(context.Context, string) (string, error) {
	client := &http.Client{Timeout: timeout}
	return func(ctx context.Context, domain string) (string, error) {
		url := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
