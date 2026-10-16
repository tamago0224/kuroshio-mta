package smtp

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

var (
	errDataLineTooLong = errors.New("data line too long")
	errMessageTooLarge = errors.New("message too large")
)

func (s *Server) handleDataCommand(
	ctx context.Context,
	ss *session,
	remoteIP net.IP,
	r *bufio.Reader,
	w *bufio.Writer,
	cmdSpan *smtpCommandSpan,
) {
	defer cmdSpan.end()
	if len(ss.rcptTo) == 0 {
		cmdSpan.setResponse(503, "need RCPT TO before DATA")
		writeResp(w, 503, "need RCPT TO before DATA")
		return
	}
	cmdSpan.addEvent("smtp.data.ready")
	writeResp(w, 354, "end with <CRLF>.<CRLF>")
	data, err := readData(r, s.cfg.MaxMessageBytes)
	if err != nil {
		cmdSpan.recordError(err)
		switch {
		case errors.Is(err, errDataLineTooLong):
			cmdSpan.reject("data_line_too_long", 500, "line too long")
			writeResp(w, 500, "line too long")
		case errors.Is(err, errMessageTooLarge):
			cmdSpan.reject("message_too_large", 552, "message size exceeds fixed maximum message size")
			writeResp(w, 552, "message size exceeds fixed maximum message size")
		default:
			cmdSpan.setResponse(451, "temporary local problem")
			writeResp(w, 451, "temporary local problem")
		}
		return
	}
	cmdSpan.SetAttributes(attribute.Int("smtp.data.bytes", len(data)))
	if !strings.EqualFold(ss.bodyMode, "8BITMIME") && contains8Bit(data) {
		cmdSpan.reject("body_requires_8bitmime", 554, "8-bit data is not permitted without BODY=8BITMIME")
		writeResp(w, 554, "8-bit data is not permitted without BODY=8BITMIME")
		ss.mailFrom = ""
		ss.rcptTo = nil
		ss.data = nil
		return
	}
	ss.data = data
	msgRemoteIP := remoteIP
	if msgRemoteIP == nil {
		msgRemoteIP = parseRemoteIP(ss.remote)
	}
	authRes := evaluateAuthWithPolicy(
		msgRemoteIP,
		ss.helo,
		ss.mailFrom,
		ss.data,
		mailauth.SPFPolicy{
			HeloMode:       s.cfg.SPFHeloPolicy,
			MailFromMode:   s.cfg.SPFMailFromPolicy,
			ARCFailureMode: s.cfg.ARCFailurePolicy,
		},
	)
	s.metricAuthResult(authRes)
	cmdSpan.SetAttributes(
		attribute.String("smtp.auth.action", string(authRes.Action)),
		attribute.String("smtp.auth.dmarc.result", authRes.DMARC.Result),
		attribute.String("smtp.auth.dmarc.policy", authRes.DMARC.Policy),
	)
	if authRes.Action == mailauth.ActionReject {
		cmdSpan.reject("auth_policy_reject", 550, "message rejected by auth policy")
		writeResp(w, 550, "message rejected by auth policy")
		ss.mailFrom = ""
		ss.rcptTo = nil
		ss.data = nil
		return
	}
	addedHeaders := []string{mailauth.BuildAuthResultsHeader(s.cfg.Hostname, authRes, ss.mailFrom)}
	if authRes.Action == mailauth.ActionQuarantine {
		addedHeaders = append(addedHeaders, "X-Kuroshio-Quarantine: true")
	}
	ss.data = mailauth.InjectHeaders(ss.data, addedHeaders)
	id, err := newID()
	if err != nil {
		cmdSpan.recordError(err)
		cmdSpan.setResponse(451, "temporary local problem")
		writeResp(w, 451, "temporary local problem")
		return
	}
	received := buildReceivedHeader(
		s.cfg.Hostname,
		ss.helo,
		ss.remote,
		id,
		time.Now().UTC(),
		ss.extended,
		ss.tls,
	)
	ss.data = mailauth.InjectHeaders(ss.data, []string{received})

	if err := s.enqueue(ctx, ss, id); err != nil {
		cmdSpan.recordError(err)
		cmdSpan.SetAttributes(attribute.String("smtp.message_id", id))
		cmdSpan.setResponse(451, "temporary local problem")
		slog.Error(
			"enqueue failed",
			"component", "smtp",
			"error", err,
			"msg_id", id,
			"remote_ip", ipString(msgRemoteIP),
		)
		s.metricInc("smtp_enqueue_fail")
		writeResp(w, 451, "temporary local problem")
		return
	}
	s.enqueueDMARCReports(
		ctx,
		authRes,
		ss.mailFrom,
		id,
		time.Now().UTC(),
	)
	s.metricInc("smtp_queued_messages")
	cmdSpan.SetAttributes(attribute.String("smtp.message_id", id))
	cmdSpan.setResponse(250, "queued")
	writeResp(w, 250, "queued")
	ss.mailFrom = ""
	ss.rcptTo = nil
	ss.data = nil
}

func readData(r *bufio.Reader, maxBytes int64) ([]byte, error) {
	var out []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) > 1000 {
			return nil, errDataLineTooLong
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		out = append(out, []byte(line)...)
		out = append(out, '\r', '\n')
		if int64(len(out)) > maxBytes {
			return nil, errMessageTooLarge
		}
	}
	return out, nil
}

func contains8Bit(data []byte) bool {
	for _, b := range data {
		if b >= 0x80 {
			return true
		}
	}
	return false
}

func (s *Server) enqueue(ctx context.Context, ss *session, id string) error {
	msg := &model.Message{
		ID:         id,
		RemoteAddr: ss.remote,
		Helo:       ss.helo,
		MailFrom:   ss.mailFrom,
		RcptTo:     append([]string(nil), ss.rcptTo...),
		Data:       append([]byte(nil), ss.data...),
	}
	observability.InjectTraceContext(ctx, msg)
	return s.queue.Enqueue(msg)
}
