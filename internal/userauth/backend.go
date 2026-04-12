package userauth

import (
	"errors"
	"strings"
)

type Principal struct {
	Username string
}

type Backend interface {
	AuthenticatePassword(username, password string) (Principal, bool)
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
	if b == nil {
		return Principal{}, false
	}
	u := normalizeUsername(username)
	pw, ok := b.users[u]
	if !ok || pw != password {
		return Principal{}, false
	}
	return Principal{Username: u}, true
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
