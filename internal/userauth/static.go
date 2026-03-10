package userauth

import (
	"errors"
	"strings"
)

type Backend interface {
	Validate(username, password string) bool
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
		u := strings.ToLower(strings.TrimSpace(p[:i]))
		pw := strings.TrimSpace(p[i+1:])
		if u == "" || pw == "" {
			return nil, errors.New("invalid submission user entry")
		}
		users[u] = pw
	}
	return &StaticBackend{users: users}, nil
}

func (b *StaticBackend) Validate(username, password string) bool {
	if b == nil {
		return false
	}
	pw, ok := b.users[strings.ToLower(strings.TrimSpace(username))]
	if !ok {
		return false
	}
	return pw == password
}
