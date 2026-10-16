package smtp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

func (s *Server) enqueueDMARCReports(ctx context.Context, authRes mailauth.Result, mailFrom, msgID string, now time.Time) {
	if s.submission || s.queue == nil {
		return
	}
	fromDomain, ok := util.DomainOf(mailFrom)
	if !ok {
		return
	}
	reports := mailauth.BuildDMARCOutboundReports(authRes.DMARC, fromDomain, s.cfg.Hostname, msgID, now)
	if len(reports) == 0 {
		return
	}
	for _, rep := range reports {
		id, err := newID()
		if err != nil {
			slog.Warn("dmarc report id generation failed", "component", "smtp", "error", err, "parent_msg_id", msgID)
			continue
		}
		reportFrom := fmt.Sprintf("postmaster@%s", s.cfg.Hostname)
		msg := &model.Message{
			ID:         id,
			RemoteAddr: s.cfg.Hostname,
			Helo:       s.cfg.Hostname,
			MailFrom:   "",
			RcptTo:     []string{rep.To},
			Data:       buildReportMessage(reportFrom, rep.To, rep.Subject, rep.Body, id, now),
		}
		observability.InjectTraceContext(ctx, msg)
		if err := s.queue.Enqueue(msg); err != nil {
			slog.Warn("enqueue dmarc report failed", "component", "smtp", "error", err, "parent_msg_id", msgID, "rcpt", logging.MaskEmail(rep.To))
			continue
		}
		s.metricInc("smtp_dmarc_report_queued")
	}
}

func buildReportMessage(from, to, subject string, body []byte, msgID string, now time.Time) []byte {
	normalizedBody := strings.ReplaceAll(string(body), "\r\n", "\n")
	normalizedBody = strings.ReplaceAll(normalizedBody, "\r", "\n")
	if !strings.HasSuffix(normalizedBody, "\n") {
		normalizedBody += "\n"
	}

	var b bytes.Buffer
	b.WriteString("From: ")
	b.WriteString(sanitizeHeaderValue(from))
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(sanitizeHeaderValue(to))
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(sanitizeHeaderValue(subject))
	b.WriteString("\r\n")
	b.WriteString("Date: ")
	b.WriteString(now.UTC().Format(time.RFC1123Z))
	b.WriteString("\r\n")
	b.WriteString("Message-ID: <")
	b.WriteString(sanitizeHeaderValue(msgID))
	b.WriteString(">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(normalizedBody, "\n", "\r\n"))
	return b.Bytes()
}
