package userauth

import "context"

// BearerBackend validates an access token for the requested mailbox.
type BearerBackend interface {
	AuthenticateBearerToken(context.Context, string, string) AuthResult
}

// MechanismBackend advertises only mechanisms supported by a backend.
type MechanismBackend interface{ AuthMechanisms() []string }

// CombinedBackend keeps password and bearer credential validation independent.
type CombinedBackend struct {
	Password Backend
	Bearer   BearerBackend
}

func (b *CombinedBackend) AuthMechanisms() []string {
	var methods []string
	if b.Password != nil {
		methods = append(methods, "PLAIN", "LOGIN")
	}
	if b.Bearer != nil {
		methods = append(methods, "XOAUTH2")
	}
	return methods
}
func (b *CombinedBackend) AuthenticatePassword(user, password string) (Principal, bool) {
	r := b.AuthenticatePasswordDetailed(user, password)
	return r.Principal, r.Success
}
func (b *CombinedBackend) AuthenticatePasswordDetailed(user, password string) AuthResult {
	if b.Password == nil {
		return AuthResult{FailureReason: "unsupported_mechanism"}
	}
	if d, ok := b.Password.(DetailedBackend); ok {
		return d.AuthenticatePasswordDetailed(user, password)
	}
	p, ok := b.Password.AuthenticatePassword(user, password)
	return AuthResult{Principal: p, Success: ok}
}
func (b *CombinedBackend) AuthenticateBearerToken(ctx context.Context, user, token string) AuthResult {
	if b.Bearer == nil {
		return AuthResult{FailureReason: "unsupported_mechanism"}
	}
	return b.Bearer.AuthenticateBearerToken(ctx, user, token)
}
