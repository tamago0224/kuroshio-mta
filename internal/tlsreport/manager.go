package tlsreport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/util"
)

type aggregate struct {
	PeriodStart   time.Time `json:"period_start"`
	PolicyDomain  string    `json:"policy_domain"`
	PolicyType    string    `json:"policy_type"`
	PolicyStrings []string  `json:"policy_strings"`
	MXHost        string    `json:"mx_host"`
	SendingMTAIP  string    `json:"sending_mta_ip,omitempty"`
	ReceivingIP   string    `json:"receiving_ip,omitempty"`
	ResultType    string    `json:"result_type,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	Successes     uint64    `json:"successes"`
	Failures      uint64    `json:"failures"`
}

type state struct {
	Aggregates map[string]aggregate `json:"aggregates"`
}

type Manager struct {
	mu           sync.Mutex
	cfg          config.TLSReportConfig
	path         string
	state        state
	metrics      *observability.Metrics
	lookupTXT    func(context.Context, string) ([]string, error)
	client       *http.Client
	now          func() time.Time
	observations chan delivery.TLSObservation
}

func Open(cfg config.TLSReportConfig, queueDir string, metrics *observability.Metrics) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, path: filepath.Join(queueDir, "tls-report-state.json"), metrics: metrics, now: time.Now, observations: make(chan delivery.TLSObservation, 1024)}
	m.state.Aggregates = map[string]aggregate{}
	m.lookupTXT = net.DefaultResolver.LookupTXT
	m.client = safeHTTPClient(15 * time.Second)
	raw, err := os.ReadFile(m.path)
	if err == nil {
		if json.Unmarshal(raw, &m.state) != nil || m.state.Aggregates == nil {
			return nil, errors.New("invalid TLS report state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return m, nil
}

// Observe queues metadata without blocking mail delivery.
func (m *Manager) Observe(ob delivery.TLSObservation) {
	select {
	case m.observations <- ob:
	default:
		m.metric("tls_report_observation_dropped")
	}
}

func (m *Manager) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case ob := <-m.observations:
					if err := m.add(ob); err != nil {
						return err
					}
				default:
					return ctx.Err()
				}
			}
		case ob := <-m.observations:
			if err := m.add(ob); err != nil {
				m.metric("tls_report_store_errors")
				slog.Warn("TLS report observation failed", "component", "tls_report", "error", err)
			}
		case <-ticker.C:
			if err := m.flush(ctx); err != nil {
				m.metric("tls_report_flush_errors")
				slog.Warn("TLS report flush failed", "component", "tls_report", "error", err)
			}
		}
	}
}

func (m *Manager) add(ob delivery.TLSObservation) error {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(ob.PolicyDomain), "."))
	if !validObservation(ob, domain) {
		return errors.New("invalid TLS report observation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	period := ob.ObservedAt.UTC().Truncate(24 * time.Hour)
	keyRaw, _ := json.Marshal([]any{period.Unix(), domain, ob.PolicyType, ob.PolicyString, strings.ToLower(ob.MXHost), ob.SendingMTAIP, ob.ReceivingIP, ob.ResultType, ob.Reason})
	keySum := sha256.Sum256(keyRaw)
	key := hex.EncodeToString(keySum[:])
	item, exists := m.state.Aggregates[key]
	if !exists && len(m.state.Aggregates) >= m.cfg.MaxRecords {
		return errors.New("TLS report aggregate limit reached")
	}
	if !exists {
		item = aggregate{PeriodStart: period, PolicyDomain: domain, PolicyType: ob.PolicyType, PolicyStrings: append([]string(nil), ob.PolicyString...), MXHost: strings.ToLower(strings.TrimSuffix(ob.MXHost, ".")), SendingMTAIP: ob.SendingMTAIP, ReceivingIP: ob.ReceivingIP, ResultType: ob.ResultType, Reason: ob.Reason}
	}
	if ob.Success {
		item.Successes++
	} else {
		item.Failures++
	}
	m.state.Aggregates[key] = item
	return m.persistLocked()
}

func validObservation(ob delivery.TLSObservation, domain string) bool {
	if ob.ObservedAt.IsZero() || !validDomain(domain) || !validDomain(ob.MXHost) || len(ob.PolicyString) > 100 {
		return false
	}
	if (ob.SendingMTAIP != "" && net.ParseIP(ob.SendingMTAIP) == nil) || (ob.ReceivingIP != "" && net.ParseIP(ob.ReceivingIP) == nil) {
		return false
	}
	if ob.PolicyType != "sts" && ob.PolicyType != "tlsa" && ob.PolicyType != "no-policy-found" {
		return false
	}
	if ob.Success {
		return ob.ResultType == "" && ob.Reason == ""
	}
	return ob.ResultType != "" && len(ob.ResultType) <= 64 && len(ob.Reason) <= 256
}

func validDomain(value string) bool {
	_, ok := util.DomainOf("x@" + strings.TrimSuffix(strings.TrimSpace(value), "."))
	return ok && !strings.ContainsAny(value, " /:@\r\n\t")
}

func (m *Manager) flush(ctx context.Context) error {
	now := m.now().UTC()
	cutoff := now.Truncate(24 * time.Hour)
	m.mu.Lock()
	type periodGroup struct {
		domain string
		period time.Time
		items  []aggregate
	}
	groups := map[string]*periodGroup{}
	for _, item := range m.state.Aggregates {
		if item.PeriodStart.Before(cutoff) {
			key := item.PolicyDomain + "\x00" + item.PeriodStart.Format(time.RFC3339)
			group := groups[key]
			if group == nil {
				group = &periodGroup{domain: item.PolicyDomain, period: item.PeriodStart}
				groups[key] = group
			}
			group.items = append(group.items, item)
		}
	}
	m.mu.Unlock()
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		endpoint, err := m.endpoint(ctx, group.domain)
		if err != nil || endpoint == "" {
			if err != nil {
				m.metric("tls_report_dns_errors")
			}
			continue
		}
		body, err := m.buildReport(group.items)
		if err != nil {
			return err
		}
		if err := m.post(ctx, endpoint, body); err != nil {
			m.metric("tls_report_delivery_retried")
			continue
		}
		m.mu.Lock()
		for key, item := range m.state.Aggregates {
			if item.PolicyDomain == group.domain && item.PeriodStart.Equal(group.period) {
				delete(m.state.Aggregates, key)
			}
		}
		err = m.persistLocked()
		m.mu.Unlock()
		if err != nil {
			return err
		}
		m.metric("tls_report_delivered")
	}
	return nil
}

func (m *Manager) endpoint(ctx context.Context, domain string) (string, error) {
	records, err := m.lookupTXT(ctx, "_smtp._tls."+domain)
	if err != nil {
		return "", err
	}
	valid := []string{}
	for _, record := range records {
		if strings.HasPrefix(record, "v=TLSRPTv1;") {
			valid = append(valid, record)
		}
	}
	if len(valid) != 1 {
		return "", nil
	}
	for _, field := range strings.Split(valid[0], ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || !strings.EqualFold(key, "rua") {
			continue
		}
		for _, raw := range strings.Split(value, ",") {
			u, parseErr := url.Parse(strings.TrimSpace(raw))
			if parseErr == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" {
				return u.String(), nil
			}
		}
	}
	return "", nil
}

type report struct {
	OrganizationName string `json:"organization-name"`
	DateRange        struct {
		Start time.Time `json:"start-datetime"`
		End   time.Time `json:"end-datetime"`
	} `json:"date-range"`
	ContactInfo string         `json:"contact-info"`
	ReportID    string         `json:"report-id"`
	Policies    []policyResult `json:"policies"`
}
type policyResult struct {
	Policy         policy          `json:"policy"`
	Summary        summary         `json:"summary"`
	FailureDetails []failureDetail `json:"failure-details,omitempty"`
}
type policy struct {
	Type    string   `json:"policy-type"`
	Strings []string `json:"policy-string"`
	Domain  string   `json:"policy-domain"`
	MXHost  []string `json:"mx-host"`
}
type summary struct {
	Success uint64 `json:"total-successful-session-count"`
	Failure uint64 `json:"total-failure-session-count"`
}
type failureDetail struct {
	ResultType          string `json:"result-type"`
	SendingMTAIP        string `json:"sending-mta-ip,omitempty"`
	ReceivingMXHostname string `json:"receiving-mx-hostname"`
	ReceivingIP         string `json:"receiving-ip,omitempty"`
	FailedSessionCount  uint64 `json:"failed-session-count"`
	Reason              string `json:"failure-reason-code,omitempty"`
}

func (m *Manager) buildReport(items []aggregate) ([]byte, error) {
	sort.Slice(items, func(i, j int) bool { return items[i].PeriodStart.Before(items[j].PeriodStart) })
	start := items[0].PeriodStart
	end := start.Add(24 * time.Hour)
	r := report{OrganizationName: m.cfg.Organization, ContactInfo: m.cfg.ContactInfo}
	r.DateRange.Start, r.DateRange.End = start, end
	sum := sha256.Sum256([]byte(m.cfg.ReportDomain + "\x00" + start.Format(time.RFC3339)))
	r.ReportID = hex.EncodeToString(sum[:16])
	groups := map[string]*policyResult{}
	for _, item := range items {
		keyRaw, _ := json.Marshal([]any{item.PolicyType, item.PolicyStrings, item.PolicyDomain, item.MXHost})
		key := string(keyRaw)
		p := groups[key]
		if p == nil {
			p = &policyResult{Policy: policy{Type: item.PolicyType, Strings: item.PolicyStrings, Domain: item.PolicyDomain, MXHost: policyMXHosts(item)}}
			groups[key] = p
		}
		p.Summary.Success += item.Successes
		p.Summary.Failure += item.Failures
		if item.Failures > 0 {
			p.FailureDetails = append(p.FailureDetails, failureDetail{ResultType: item.ResultType, SendingMTAIP: item.SendingMTAIP, ReceivingMXHostname: item.MXHost, ReceivingIP: item.ReceivingIP, FailedSessionCount: item.Failures, Reason: item.Reason})
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r.Policies = append(r.Policies, *groups[key])
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(body) > m.cfg.MaxReportBytes {
		return nil, errors.New("TLS report exceeds maximum size")
	}
	return body, nil
}

func policyMXHosts(item aggregate) []string {
	if item.PolicyType != "sts" {
		return []string{item.MXHost}
	}
	hosts := []string{}
	for _, line := range item.PolicyStrings {
		if value, ok := strings.CutPrefix(line, "mx: "); ok {
			hosts = append(hosts, value)
		}
	}
	return hosts
}

func (m *Manager) post(ctx context.Context, endpoint string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/tlsrpt+json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("TLS report endpoint status %d", resp.StatusCode)
	}
	return nil
}

func safeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if unsafeIP(ip) {
				return nil, errors.New("unsafe TLS report endpoint address")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("TLS report endpoint has no address")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func (m *Manager) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".tls-report-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, m.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(m.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *Manager) metric(name string) {
	if m.metrics != nil {
		m.metrics.Counter(name).Inc()
	}
}
