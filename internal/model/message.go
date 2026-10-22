package model

import "time"

type Message struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RemoteAddr  string    `json:"remote_addr"`
	Helo        string    `json:"helo"`
	MailFrom    string    `json:"mail_from"`
	RcptTo      []string  `json:"rcpt_to"`
	Data        []byte    `json:"data"`
	TraceParent string    `json:"trace_parent,omitempty"`
	TraceState  string    `json:"trace_state,omitempty"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error"`
	// Tenant and TrafficClass are trusted queue metadata assigned by authenticated ingress or the system.
	Tenant       string `json:"tenant,omitempty"`
	TrafficClass string `json:"traffic_class,omitempty"`
	// Quarantine is trusted queue metadata, never inferred from message headers.
	Quarantine *Quarantine `json:"quarantine,omitempty"`

	// SigningIdentity is trusted ingress or system metadata; nil denotes legacy/unknown origin.
	SigningIdentity *SigningIdentity `json:"signing_identity,omitempty"`
}

// Quarantine records a policy hold. Only an explicit operator release clears it.
type Quarantine struct {
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SigningIdentity with an empty Domain explicitly denotes unsigned inbound mail.
// A nonempty domain is granted only by authenticated ingress sender scope.
type SigningIdentity struct {
	Domain string `json:"domain,omitempty"`
}
