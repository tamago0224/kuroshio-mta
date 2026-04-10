package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

type Principal struct {
	Role role
}

type AuthBackend interface {
	AuthenticateBearerToken(token string) (Principal, bool)
}

type configTokenBackend struct {
	plain  map[string]Principal
	hashed []hashedToken
}

type hashedToken struct {
	sum       [32]byte
	principal Principal
}

func NewConfigTokenBackend(v string) AuthBackend {
	out := &configTokenBackend{plain: map[string]Principal{}}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokenSpec, roleSpec, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		tokenSpec = strings.TrimSpace(tokenSpec)
		r := role(strings.ToLower(strings.TrimSpace(roleSpec)))
		switch r {
		case roleViewer, roleOperator, roleAdmin:
		default:
			continue
		}
		if tokenSpec == "" {
			continue
		}
		p := Principal{Role: r}
		if strings.HasPrefix(strings.ToLower(tokenSpec), "sha256=") {
			raw := strings.TrimSpace(tokenSpec[len("sha256="):])
			sum, ok := decodeSHA256Hex(raw)
			if !ok {
				continue
			}
			out.hashed = append(out.hashed, hashedToken{sum: sum, principal: p})
			continue
		}
		out.plain[tokenSpec] = p
	}
	return out
}

func (b *configTokenBackend) AuthenticateBearerToken(token string) (Principal, bool) {
	if b == nil {
		return Principal{}, false
	}
	if got, ok := b.plain[token]; ok {
		return got, true
	}
	sum := sha256.Sum256([]byte(token))
	for _, hashed := range b.hashed {
		if subtle.ConstantTimeCompare(sum[:], hashed.sum[:]) == 1 {
			return hashed.principal, true
		}
	}
	return Principal{}, false
}

func decodeSHA256Hex(v string) ([32]byte, bool) {
	var out [32]byte
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != len(out) {
		return out, false
	}
	copy(out[:], b)
	return out, true
}
