package smtp

import (
	"bufio"
	"context"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/policyhook"
)

// applyPolicyHook can add a refusal only after built-in checks have passed.
func (s *Server) applyPolicyHook(ctx context.Context, ss *session, stage, recipient string, w *bufio.Writer, span *smtpCommandSpan) bool {
	if s.policyHook == nil {
		return true
	}
	request := policyhook.Request{Stage: stage, RemoteIP: ipString(parseRemoteIP(ss.remote)), HELO: ss.helo, MailFrom: ss.mailFrom, Recipients: append([]string{}, ss.rcptTo...), Recipient: recipient, TLS: ss.tls, Submission: s.submission, Authenticated: ss.authOK}
	if stage == "data" {
		request.MessageBytes = len(ss.data)
		if s.cfg.PolicyHook.SendBody {
			request.Message = ss.data
		}
	}
	action, err := s.policyHook.Evaluate(ctx, request)
	if err != nil {
		action = policyhook.Tempfail
	}
	switch action {
	case policyhook.Accept:
		return true
	case policyhook.Quarantine:
		if stage == "data" && s.cfg.Quarantine.Enabled {
			if ss.quarantine == nil {
				ss.quarantine = &model.Quarantine{Reason: "policy_hook", ExpiresAt: time.Now().UTC().Add(s.cfg.Quarantine.Retention)}
			}
			return true
		}
		span.reject("policy_hook_quarantine_unavailable", 451, "policy quarantine unavailable")
		writeResp(w, 451, "4.3.0 policy quarantine unavailable")
	case policyhook.Reject:
		code := 550
		if stage == "data" {
			code = 554
		}
		span.reject("policy_hook_reject", code, "rejected by policy")
		writeResp(w, code, "5.7.1 rejected by policy")
	default:
		span.reject("policy_hook_tempfail", 451, "policy temporarily unavailable")
		writeResp(w, 451, "4.3.0 policy temporarily unavailable")
	}
	return false
}
