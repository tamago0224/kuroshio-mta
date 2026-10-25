package userauth

import (
	"errors"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/util"
)

type AuthResult struct {
	Principal     Principal
	Success       bool
	FailureReason string
}

// SenderAllowed reports whether an authenticated principal may use mailFrom.
// Explicit address/domain grants take precedence over the legacy username-domain fallback.
func SenderAllowed(principal Principal, mailFrom string) bool {
	mailFrom = strings.ToLower(strings.TrimSpace(mailFrom))
	if mailFrom == "" {
		return false
	}
	for _, allowed := range principal.AllowedSenderAddresses {
		if strings.EqualFold(strings.TrimSpace(allowed), mailFrom) {
			return true
		}
	}
	if fromDomain, ok := util.DomainOf(mailFrom); ok {
		for _, allowed := range principal.AllowedSenderDomains {
			if strings.EqualFold(strings.TrimSpace(allowed), fromDomain) {
				return true
			}
		}
	}
	if principal.RequireExplicitSenderScope {
		return false
	}
	authDomain, ok := util.DomainOf(strings.ToLower(strings.TrimSpace(principal.Username)))
	if !ok {
		return false
	}
	fromDomain, ok := util.DomainOf(mailFrom)
	return ok && strings.EqualFold(authDomain, fromDomain)
}

type Principal struct {
	// RequireExplicitSenderScope disables the legacy username-domain fallback.
	RequireExplicitSenderScope bool
	AuthSource                 string
	Username                   string
	AllowedSenderDomains       []string
	AllowedSenderAddresses     []string
}

type Backend interface {
	AuthenticatePassword(username, password string) (Principal, bool)
}

type DetailedBackend interface {
	AuthenticatePasswordDetailed(username, password string) AuthResult
}

type StaticBackend struct {
	users map[string]string
}

func NewStatic(raw string) (*StaticBackend, error) {
	users := map[string]string{}
	s := strings.TrimSpace(raw)
	if s == "" {
		return &StaticBackend{users: users}, nil
	}
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		i := strings.IndexByte(p, ':')
		if i <= 0 || i == len(p)-1 {
			return nil, errors.New("invalid submission user entry")
		}
		u := normalizeUsername(p[:i])
		pw := strings.TrimSpace(p[i+1:])
		if u == "" || pw == "" {
			return nil, errors.New("invalid submission user entry")
		}
		users[u] = pw
	}
	return &StaticBackend{users: users}, nil
}

func (b *StaticBackend) AuthenticatePassword(username, password string) (Principal, bool) {
	result := b.AuthenticatePasswordDetailed(username, password)
	return result.Principal, result.Success
}

func (b *StaticBackend) AuthenticatePasswordDetailed(username, password string) AuthResult {
	if b == nil {
		return AuthResult{FailureReason: "backend_unavailable"}
	}
	u := normalizeUsername(username)
	pw, ok := b.users[u]
	if !ok {
		return AuthResult{
			Principal:     Principal{AuthSource: "static_password", Username: u},
			FailureReason: "credential_not_found",
		}
	}
	if pw != password {
		return AuthResult{
			Principal:     Principal{AuthSource: "static_password", Username: u},
			FailureReason: "invalid_password",
		}
	}
	return AuthResult{
		Principal: Principal{
			AuthSource: "static_password",
			Username:   u,
		},
		Success: true,
	}
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
