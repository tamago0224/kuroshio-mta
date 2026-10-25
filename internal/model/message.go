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
	DSN         *DSN      `json:"dsn,omitempty"`
	RequireTLS  bool      `json:"require_tls,omitempty"`
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

// DSN records RFC 3461 envelope parameters. Recipient keys are normalized
// envelope addresses from RcptTo; missing entries retain the RFC defaults.
type DSN struct {
	Return     string                         `json:"return,omitempty"`
	EnvelopeID string                         `json:"envelope_id,omitempty"`
	Recipients map[string]DSNRecipientOptions `json:"recipients,omitempty"`
}

type DSNRecipientOptions struct {
	Notify            []string `json:"notify,omitempty"`
	OriginalRecipient string   `json:"original_recipient,omitempty"`
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
