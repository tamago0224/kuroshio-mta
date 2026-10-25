package smtp

import (
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
)

type session struct {
	signing         *model.SigningIdentity
	quarantine      *model.Quarantine
	closeConnection bool
	authFailures    int
	rcptFailures    int
	messages        int
	remote          string
	helo            string
	mailFrom        string
	bodyMode        string
	dsn             *model.DSN
	rcptTo          []string
	data            []byte
	seenHelo        bool
	extended        bool
	tls             bool
	authUser        string
	auth            userauth.Principal
	authOK          bool
}

// finishData clears a completed or rejected message, preserving the BODY mode.
// Temporary DATA failures retain the transaction and do not call this method.
func (ss *session) finishData() {
	ss.mailFrom = ""
	ss.rcptTo = nil
	ss.data = nil
	ss.quarantine = nil
	ss.signing = nil
	ss.dsn = nil
}

// resetTransaction preserves the greeting, authentication and connection state.
func (ss *session) resetTransaction() {
	ss.finishData()
	ss.bodyMode = "7BIT"
}

func (ss *session) beginTransaction(args mailFromArgs) {
	ss.resetTransaction()
	ss.mailFrom = args.Address
	ss.dsn = args.DSN
	if args.Body != "" {
		ss.bodyMode = args.Body
	}
}

// The caller sets helo before accepting the greeting (and before EHLO rate checks).
func (ss *session) acceptGreeting(extended bool) {
	ss.resetTransaction()
	ss.seenHelo = true
	ss.extended = extended
}

func (ss *session) resetAfterTLS() {
	ss.resetTransaction()
	ss.tls = true
	ss.seenHelo = false
	ss.extended = false
	ss.authUser = ""
	ss.auth = userauth.Principal{}
	ss.authOK = false
	// Keep remote and the previous helo value, matching the existing session behavior.
}
