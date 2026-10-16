package reputation

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const MaxReportBytes = 1 << 20

var ErrInvalidDomain = errors.New("invalid reputation domain")

type ReportCounts struct {
	Complaints uint64
	Successes  uint64
	Failures   uint64
}

type Report struct {
	ID     string
	Counts map[string]ReportCounts
}

func validReportDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || domain != strings.ToLower(domain) {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
		}
	}
	return true
}

func reportIdentity(kind, id string) string {
	hash := sha256.Sum256([]byte(kind + "\x00" + id))
	return hex.EncodeToString(hash[:])
}

// ParseTLSReport accepts bounded RFC 8460 JSON, optionally gzip compressed.
func ParseTLSReport(raw []byte, contentType string, now time.Time) (Report, error) {
	if len(raw) > MaxReportBytes {
		return Report{}, errors.New("report exceeds size limit")
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Report{}, errors.New("invalid report content type")
	}
	if media == "application/tlsrpt+gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return Report{}, errors.New("invalid gzip report")
		}
		defer reader.Close()
		raw, err = io.ReadAll(io.LimitReader(reader, MaxReportBytes+1))
		if err != nil || len(raw) > MaxReportBytes {
			return Report{}, errors.New("invalid or oversized gzip report")
		}
	} else if media != "application/tlsrpt+json" && media != "application/json" {
		return Report{}, errors.New("unsupported TLS report content type")
	}
	var payload struct {
		Organization string `json:"organization-name"`
		ID           string `json:"report-id"`
		DateRange    struct {
			Start time.Time `json:"start-datetime"`
			End   time.Time `json:"end-datetime"`
		} `json:"date-range"`
		Policies []struct {
			Policy struct {
				Domain string `json:"policy-domain"`
			} `json:"policy"`
			Summary struct {
				Successes *uint64 `json:"total-successful-session-count"`
				Failures  *uint64 `json:"total-failure-session-count"`
			} `json:"summary"`
		} `json:"policies"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Report{}, errors.New("invalid TLS report JSON")
	}
	invalidIdentity := strings.TrimSpace(payload.Organization) == "" || len(payload.Organization) > 256 || strings.TrimSpace(payload.ID) == "" || len(payload.ID) > 256
	validRange := !payload.DateRange.Start.IsZero() && payload.DateRange.End.After(payload.DateRange.Start)
	recent := !payload.DateRange.Start.Before(now.Add(-30*24*time.Hour)) && !payload.DateRange.End.After(now.Add(24*time.Hour))
	invalidIdentity = invalidIdentity || strings.ContainsAny(payload.Organization+payload.ID, "\x00\r\n")
	if invalidIdentity || !validRange || !recent || len(payload.Policies) == 0 || len(payload.Policies) > 100 {
		return Report{}, errors.New("invalid TLS report identity, period, or policies")
	}
	report := Report{ID: reportIdentity("tlsrpt", payload.Organization+"\x00"+payload.ID), Counts: make(map[string]ReportCounts)}
	for _, policy := range payload.Policies {
		domain := normalizeDomain(policy.Policy.Domain)
		if !validReportDomain(domain) || policy.Summary.Successes == nil || policy.Summary.Failures == nil {
			return Report{}, errors.New("invalid TLS report policy or counts")
		}
		counts := report.Counts[domain]
		if ^uint64(0)-counts.Successes < *policy.Summary.Successes || ^uint64(0)-counts.Failures < *policy.Summary.Failures {
			return Report{}, errors.New("TLS report count overflow")
		}
		counts.Successes += *policy.Summary.Successes
		counts.Failures += *policy.Summary.Failures
		report.Counts[domain] = counts
	}
	return report, nil
}

// ParseComplaint accepts a feedback-report part or a multipart/report body.
// The trusted collector supplies a stable globally unique report ID.
func ParseComplaint(raw []byte, contentType, id string) (Report, error) {
	if len(raw) > MaxReportBytes || id == "" || len(id) > 256 {
		return Report{}, errors.New("invalid complaint size or id")
	}
	for _, ch := range id {
		if ch < 33 || ch > 126 {
			return Report{}, errors.New("invalid complaint id")
		}
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Report{}, errors.New("invalid complaint content type")
	}
	switch media {
	case "multipart/report":
		if !strings.EqualFold(params["report-type"], "feedback-report") || params["boundary"] == "" {
			return Report{}, errors.New("invalid complaint multipart type")
		}
		reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
		var feedback []byte
		found := false
		for i := 0; ; i++ {
			part, err := reader.NextRawPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return Report{}, errors.New("invalid complaint multipart")
			}
			if i >= 10 {
				part.Close()
				return Report{}, errors.New("too many complaint parts")
			}
			media, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if media != "message/feedback-report" {
				part.Close()
				continue
			}
			if found {
				part.Close()
				return Report{}, errors.New("multiple complaint feedback parts")
			}
			encoding := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))
			if encoding != "" && encoding != "7bit" && encoding != "8bit" {
				part.Close()
				return Report{}, errors.New("unsupported feedback encoding")
			}
			feedback, err = io.ReadAll(io.LimitReader(part, MaxReportBytes+1))
			part.Close()
			if err != nil || len(feedback) > MaxReportBytes {
				return Report{}, errors.New("invalid complaint feedback part")
			}
			found = true
		}
		if !found {
			return Report{}, errors.New("missing complaint feedback part")
		}
		raw = feedback
	case "message/feedback-report":
	default:
		return Report{}, errors.New("unsupported complaint content type")
	}
	// A feedback report is an RFC 5322 style header block, not the original email.
	normalized := strings.TrimRight(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	headers, err := textproto.NewReader(bufio.NewReader(strings.NewReader(normalized + "\n\n"))).ReadMIMEHeader()
	invalidFields := len(headers.Values("Feedback-Type")) != 1 || len(headers.Values("Version")) != 1 || len(headers.Values("User-Agent")) != 1
	if err != nil || invalidFields || !strings.EqualFold(headers.Get("Feedback-Type"), "abuse") || headers.Get("Version") != "1" || headers.Get("User-Agent") == "" {
		return Report{}, errors.New("invalid abuse feedback fields")
	}
	if len(headers.Values("Incidents")) > 1 {
		return Report{}, errors.New("duplicate incident count")
	}
	count := uint64(1)
	if incidents := headers.Get("Incidents"); incidents != "" {
		count, err = strconv.ParseUint(incidents, 10, 64)
		if err != nil || count == 0 {
			return Report{}, errors.New("invalid incident count")
		}
	}
	report := Report{ID: reportIdentity("complaint", id), Counts: make(map[string]ReportCounts)}
	for _, value := range headers.Values("Original-Rcpt-To") {
		if prefix, tail, ok := strings.Cut(value, ";"); ok {
			if !strings.EqualFold(strings.TrimSpace(prefix), "rfc822") {
				return Report{}, errors.New("unsupported recipient type")
			}
			value = strings.TrimSpace(tail)
		}
		address, err := mail.ParseAddress(value)
		if err != nil {
			return Report{}, errors.New("invalid complaint recipient")
		}
		at := strings.LastIndexByte(address.Address, '@')
		if at < 0 {
			return Report{}, errors.New("missing complaint recipient domain")
		}
		domain := normalizeDomain(address.Address[at+1:])
		if !validReportDomain(domain) {
			return Report{}, errors.New("invalid complaint domain")
		}
		// One complaint per reported domain, even if the recipient field repeats.
		report.Counts[domain] = ReportCounts{Complaints: count}
	}
	if len(report.Counts) == 0 || len(report.Counts) > 100 {
		return Report{}, fmt.Errorf("complaint requires 1 to 100 recipient domains")
	}
	return report, nil
}
