package delivery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func (c *Client) deliverLMTP(ctx context.Context, socket string, msg *model.Message, recipients []string) map[string]error {
	results := make(map[string]error, len(recipients))
	setAll := func(err error) map[string]error {
		for _, rcpt := range recipients {
			if _, exists := results[rcpt]; !exists {
				results[rcpt] = err
			}
		}
		return results
	}
	dialer := net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", strings.TrimSpace(socket))
	if err != nil {
		return setAll(atStage(StageConnect, err))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.SendTimeout))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if err := expectCode(r, 220); err != nil {
		return setAll(atStage(StageGreeting, err))
	}
	if err := writeLine(w, "LHLO "+c.cfg.Hostname); err != nil {
		return setAll(atStage(StageEHLO, err))
	}
	ehloLines, err := readLines(r)
	if err != nil {
		return setAll(atStage(StageEHLO, err))
	}
	dsnSupported := supportsExtension(ehloLines, "DSN")
	mailCommand := fmt.Sprintf("MAIL FROM:<%s>", msg.MailFrom)
	if dsnSupported && msg.DSN != nil {
		if msg.DSN.Return != "" {
			mailCommand += " RET=" + msg.DSN.Return
		}
		if msg.DSN.EnvelopeID != "" {
			mailCommand += " ENVID=" + msg.DSN.EnvelopeID
		}
	}
	if err := writeLine(w, mailCommand); err != nil {
		return setAll(atStage(StageMAIL, err))
	}
	if err := expect2xx(r); err != nil {
		return setAll(atStage(StageMAIL, err))
	}
	accepted := make([]string, 0, len(recipients))
	for _, rcpt := range recipients {
		rcptCommand := fmt.Sprintf("RCPT TO:<%s>", rcpt)
		if dsnSupported && msg.DSN != nil {
			if options, ok := msg.DSN.Recipients[rcpt]; ok {
				if len(options.Notify) > 0 {
					rcptCommand += " NOTIFY=" + strings.Join(options.Notify, ",")
				}
				if options.OriginalRecipient != "" {
					rcptCommand += " ORCPT=" + options.OriginalRecipient
				}
			}
		}
		if err := writeLine(w, rcptCommand); err != nil {
			return setAll(atStage(StageRCPT, err))
		}
		if err := expect2xx3xx(r); err != nil {
			results[rcpt] = fmt.Errorf("rcpt rejected: %w", atStage(StageRCPT, err))
			continue
		}
		accepted = append(accepted, rcpt)
	}
	if len(accepted) == 0 {
		return results
	}
	if err := writeLine(w, "DATA"); err != nil {
		return setAll(atStage(StageDATA, err))
	}
	if err := expectCode(r, 354); err != nil {
		return setAll(atStage(StageDATA, err))
	}
	payload, err := c.prepareOutboundMessage(ctx, msg)
	if err != nil {
		return setAll(err)
	}
	if _, err := w.Write(dotStuff(payload)); err != nil {
		return setAll(atStage(StageDATA, err))
	}
	if _, err := w.WriteString("\r\n.\r\n"); err != nil {
		return setAll(atStage(StageDATA, err))
	}
	if err := w.Flush(); err != nil {
		return setAll(atStage(StageDATA, err))
	}
	for _, rcpt := range accepted {
		code, line, err := readCode(r)
		if err != nil {
			results[rcpt] = atStage(StageDATA, err)
			for _, pending := range accepted {
				if _, exists := results[pending]; !exists {
					results[pending] = atStage(StageDATA, errors.New("LMTP final response missing"))
				}
			}
			break
		}
		if code < 200 || code >= 300 {
			results[rcpt] = &SMTPResponseError{Stage: StageDATA, Code: code, Line: line, EnhancedStatus: responseEnhancedStatus(code, line)}
		} else {
			results[rcpt] = nil
		}
	}
	_ = writeLine(w, "QUIT")
	return results
}
