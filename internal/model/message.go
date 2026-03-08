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
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error"`
}
