package dmarcreport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/util"

	_ "modernc.org/sqlite"
)

const maxTuples = 100_000

var ErrLimit = errors.New("DMARC report aggregate limit reached")

// Observation contains domain-level authentication data only. It deliberately
// has no recipient address, message body, or message ID fields.
type Observation struct {
	ObservedAt   time.Time    `json:"observed_at"`
	PolicyDomain string       `json:"policy_domain"`
	AuthorDomain string       `json:"author_domain"`
	Policy       Policy       `json:"policy"`
	Discovery    string       `json:"discovery_method"`
	SourceIP     string       `json:"source_ip"`
	Disposition  string       `json:"disposition"`
	DKIMAligned  string       `json:"dkim_aligned"`
	SPFAligned   string       `json:"spf_aligned"`
	EnvelopeFrom string       `json:"envelope_from"`
	EnvelopeTo   string       `json:"envelope_to"`
	DKIMResults  []DKIMResult `json:"dkim_results,omitempty"`
	SPFDomain    string       `json:"spf_domain,omitempty"`
	SPFResult    string       `json:"spf_result,omitempty"`
	Recipients   []string     `json:"recipients,omitempty"`
}

type Policy struct {
	P       string `json:"p"`
	SP      string `json:"sp,omitempty"`
	NP      string `json:"np,omitempty"`
	FO      string `json:"fo,omitempty"`
	ADKIM   string `json:"adkim,omitempty"`
	ASPF    string `json:"aspf,omitempty"`
	Testing string `json:"testing,omitempty"`
}

type DKIMResult struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
	Result   string `json:"result"`
}

type Aggregate struct {
	Observation Observation `json:"observation"`
	Count       uint64      `json:"count"`
}

// Store is a local durable aggregation store for a single receiver process.
type Store struct{ db *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("DMARC report store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create DMARC report state directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open DMARC report state: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect DMARC report state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("protect DMARC report state: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure DMARC report state: %w", err)
	}
	if err := initSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func initSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DMARC report schema transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dmarc_aggregate (
  tuple_key TEXT PRIMARY KEY,
  period_start INTEGER NOT NULL,
  policy_domain TEXT NOT NULL,
  observation_json BLOB NOT NULL,
  message_count INTEGER NOT NULL CHECK(message_count > 0)
)`); err != nil {
		return fmt.Errorf("create DMARC aggregate table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS dmarc_aggregate_period ON dmarc_aggregate(period_start, policy_domain)`); err != nil {
		return fmt.Errorf("index DMARC aggregate table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DMARC report schema: %w", err)
	}
	return nil
}

func (s *Store) Add(ctx context.Context, ob Observation) error {
	ob.ObservedAt = ob.ObservedAt.UTC().Truncate(24 * time.Hour)
	sort.Strings(ob.Recipients)
	if len(ob.Recipients) > 1 {
		unique := ob.Recipients[:1]
		for _, recipient := range ob.Recipients[1:] {
			if recipient != unique[len(unique)-1] {
				unique = append(unique, recipient)
			}
		}
		ob.Recipients = unique
	}
	if err := validateObservation(ob); err != nil {
		return err
	}
	data, err := json.Marshal(ob)
	if err != nil {
		return fmt.Errorf("encode DMARC observation: %w", err)
	}
	key := observationKey(ob)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DMARC aggregate update: %w", err)
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dmarc_aggregate WHERE tuple_key = ?)`, key).Scan(&exists); err != nil {
		return fmt.Errorf("check DMARC aggregate tuple: %w", err)
	}
	if !exists {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM dmarc_aggregate`).Scan(&count); err != nil {
			return fmt.Errorf("count DMARC aggregate tuples: %w", err)
		}
		if count >= maxTuples {
			return ErrLimit
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO dmarc_aggregate(tuple_key, period_start, policy_domain, observation_json, message_count)
VALUES(?, ?, ?, ?, 1)
ON CONFLICT(tuple_key) DO UPDATE SET message_count = message_count + 1`, key, ob.ObservedAt.Unix(), ob.PolicyDomain, data)
	if err != nil {
		return fmt.Errorf("write DMARC aggregate tuple: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DMARC aggregate tuple: %w", err)
	}
	return nil
}

// Snapshot returns a stable period copy. Delivery is at-least-once: callers
// delete the period after queueing the report, using deterministic report IDs.
func (s *Store) Snapshot(ctx context.Context, period time.Time) ([]Aggregate, error) {
	start := period.UTC().Truncate(24 * time.Hour).Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT observation_json, message_count FROM dmarc_aggregate WHERE period_start = ? ORDER BY policy_domain, tuple_key`, start)
	if err != nil {
		return nil, fmt.Errorf("query DMARC aggregate period: %w", err)
	}
	defer rows.Close()
	var out []Aggregate
	for rows.Next() {
		var raw []byte
		var count int64
		if err := rows.Scan(&raw, &count); err != nil {
			return nil, fmt.Errorf("scan DMARC aggregate tuple: %w", err)
		}
		var ob Observation
		if err := json.Unmarshal(raw, &ob); err != nil {
			return nil, errors.New("decode DMARC aggregate tuple")
		}
		if count <= 0 {
			return nil, errors.New("invalid DMARC aggregate count")
		}
		out = append(out, Aggregate{Observation: ob, Count: uint64(count)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DMARC aggregate tuples: %w", err)
	}
	return out, nil
}

func (s *Store) DeletePeriod(ctx context.Context, period time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dmarc_aggregate WHERE period_start = ?`, period.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return fmt.Errorf("delete DMARC aggregate period: %w", err)
	}
	return nil
}

func (s *Store) PeriodsBefore(ctx context.Context, before time.Time) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT period_start FROM dmarc_aggregate WHERE period_start < ? ORDER BY period_start`, before.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return nil, fmt.Errorf("list DMARC aggregate periods: %w", err)
	}
	defer rows.Close()
	var periods []time.Time
	for rows.Next() {
		var stamp int64
		if err := rows.Scan(&stamp); err != nil {
			return nil, fmt.Errorf("scan DMARC aggregate period: %w", err)
		}
		periods = append(periods, time.Unix(stamp, 0).UTC())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DMARC aggregate periods: %w", err)
	}
	return periods, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Flush queues reports for completed UTC days, then removes their aggregates.
// Stable queue IDs make retries idempotent on local storage; Kafka remains
// at-least-once and the RFC report ID lets consumers recognize duplicates.
func (s *Store) Flush(ctx context.Context, backend queue.Backend, receiver, contact string, now time.Time) error {
	periods, err := s.PeriodsBefore(ctx, now.UTC().Truncate(24*time.Hour))
	if err != nil {
		return err
	}
	for _, period := range periods {
		rows, err := s.Snapshot(ctx, period)
		if err != nil {
			return err
		}
		groups := make(map[string]*flushGroup)
		for _, row := range rows {
			for _, destination := range row.Observation.Recipients {
				to, ok := parseMailto(destination)
				if !ok {
					continue
				}
				keyBytes, _ := json.Marshal(struct {
					PolicyDomain, Discovery, To string
					Policy                      Policy
				}{row.Observation.PolicyDomain, row.Observation.Discovery, to, row.Observation.Policy})
				key := string(keyBytes)
				group := groups[key]
				if group == nil {
					group = &flushGroup{policyDomain: row.Observation.PolicyDomain, discovery: row.Observation.Discovery, to: to, policy: row.Observation.Policy}
					groups[key] = group
				}
				group.rows = append(group.rows, row)
			}
		}
		for _, group := range groups {
			if err := s.enqueueGroup(ctx, backend, receiver, contact, period, group); err != nil {
				return err
			}
		}
		if err := s.DeletePeriod(ctx, period); err != nil {
			return err
		}
	}
	return nil
}

type flushGroup struct {
	policyDomain, discovery, to string
	policy                      Policy
	rows                        []Aggregate
}

func (s *Store) enqueueGroup(ctx context.Context, backend queue.Backend, receiver, contact string, period time.Time, group *flushGroup) error {
	const maxRowsPerReport = 1000
	for offset := 0; offset < len(group.rows); offset += maxRowsPerReport {
		end := offset + maxRowsPerReport
		if end > len(group.rows) {
			end = len(group.rows)
		}
		if err := s.enqueueChunk(ctx, backend, receiver, contact, period, group, group.rows[offset:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) enqueueChunk(ctx context.Context, backend queue.Backend, receiver, contact string, period time.Time, group *flushGroup, rows []Aggregate) error {
	seed, _ := json.Marshal(struct {
		Period           int64
		PolicyDomain, To string
		Policy           Policy
		Rows             []Aggregate
	}{period.Unix(), group.policyDomain, group.to, group.policy, rows})
	digest := sha256.Sum256(seed)
	suffix := hex.EncodeToString(digest[:8])
	reportID := fmt.Sprintf("%d-%s-%s@%s", period.Unix(), group.policyDomain, suffix, receiver)
	raw, err := BuildXML(receiver, contact, reportID, period, group.policy, group.discovery, rows)
	if err != nil {
		return err
	}
	if len(raw) > 5<<20 {
		if len(rows) == 1 {
			return errors.New("single DMARC report tuple exceeds size limit")
		}
		mid := len(rows) / 2
		if err := s.enqueueChunk(ctx, backend, receiver, contact, period, group, rows[:mid]); err != nil {
			return err
		}
		return s.enqueueChunk(ctx, backend, receiver, contact, period, group, rows[mid:])
	}
	compressed, err := GzipXML(raw)
	if err != nil {
		return fmt.Errorf("compress DMARC aggregate report: %w", err)
	}
	filename := fmt.Sprintf("%s!%s!%d!%d!%s.xml.gz", receiver, group.policyDomain, period.Unix(), period.Add(24*time.Hour).Unix()-1, suffix)
	messageID := hex.EncodeToString(digest[:16])
	message := &model.Message{ID: messageID, RemoteAddr: receiver, Helo: receiver, MailFrom: "", RcptTo: []string{group.to}, SigningIdentity: &model.SigningIdentity{Domain: receiver}, Data: buildAggregateMessage("postmaster@"+receiver, group.to, group.policyDomain, receiver, reportID, filename, compressed, period, messageID)}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := backend.Enqueue(message); err != nil {
		return fmt.Errorf("enqueue DMARC aggregate report for %s: %w", group.policyDomain, err)
	}
	return nil
}

func buildAggregateMessage(from, to, policyDomain, receiver, reportID, filename string, attachment []byte, period time.Time, messageID string) []byte {
	boundary := "kuroshio-dmarc-" + messageID
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: Report Domain: %s Submitter: %s Report-ID: <%s>\r\nAuto-Submitted: auto-generated\r\nX-Kuroshio-DMARC-Report: aggregate\r\n", from, to, policyDomain, receiver, reportID)
	fmt.Fprintf(&b, "Date: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", period.Add(24*time.Hour-1).Format(time.RFC1123Z), messageID, receiver, boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 7bit\r\n\r\nDMARC aggregate report for %s, period starting %s UTC.\r\n", boundary, policyDomain, period.UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: application/gzip\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"%s\"\r\n\r\n", boundary, filename)
	encoded := base64.StdEncoding.EncodeToString(attachment)
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}

func observationKey(ob Observation) string {
	canonical, _ := json.Marshal(ob)
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:])
}

func validateObservation(ob Observation) error {
	if ob.ObservedAt.IsZero() || !validDomain(ob.PolicyDomain) || !validDomain(ob.AuthorDomain) {
		return errors.New("invalid DMARC report domain or timestamp")
	}
	if net.ParseIP(ob.SourceIP) == nil {
		return errors.New("invalid DMARC report source IP")
	}
	if !oneOf(ob.Discovery, "treewalk", "psl") || !oneOf(ob.Disposition, "none", "quarantine", "reject") || !oneOf(ob.DKIMAligned, "pass", "fail") || !oneOf(ob.SPFAligned, "pass", "fail") {
		return errors.New("invalid DMARC evaluated result")
	}
	if !oneOf(ob.Policy.P, "none", "quarantine", "reject") {
		return errors.New("invalid DMARC policy")
	}
	if len(ob.DKIMResults) > 100 {
		return errors.New("too many DKIM results")
	}
	for _, result := range ob.DKIMResults {
		if !validDomain(result.Domain) || !validDomain(result.Selector) || !oneOf(result.Result, "pass", "fail", "policy", "neutral", "temperror", "permerror", "none") {
			return errors.New("invalid DKIM report result")
		}
	}
	for _, value := range []string{ob.EnvelopeFrom, ob.EnvelopeTo, ob.SPFDomain} {
		if value != "" && !validDomain(value) {
			return errors.New("invalid report identifier domain")
		}
	}
	if ob.SPFResult != "" && !oneOf(ob.SPFResult, "pass", "fail", "softfail", "neutral", "none", "temperror", "permerror") {
		return errors.New("invalid SPF report result")
	}
	if len(ob.Recipients) > 20 {
		return errors.New("too many DMARC report destinations")
	}
	for _, recipient := range ob.Recipients {
		if _, ok := parseMailto(recipient); !ok {
			return errors.New("invalid DMARC report destination")
		}
	}
	return nil
}

func parseMailto(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 7 || !strings.EqualFold(value[:7], "mailto:") {
		return "", false
	}
	address := strings.TrimSpace(value[7:])
	if i := strings.IndexByte(address, '!'); i >= 0 {
		address = address[:i]
	}
	normalized, err := util.NormalizePath(address)
	if err != nil || normalized == "" {
		return "", false
	}
	return normalized, true
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.ToLower(domain) != domain {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}
func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, c := range label {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
