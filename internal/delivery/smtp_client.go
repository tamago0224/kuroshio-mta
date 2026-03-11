package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/dkim"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/router"
	"github.com/tamago0224/orinoco-mta/internal/util"
)

type messageSigner interface {
	Sign(raw []byte) ([]byte, error)
}

type Client struct {
	cfg           config.Config
	dane          *DANEResolver
	mtaSTS        *MTASTSResolver
	signer        messageSigner
	resolveMXFn   func(string, time.Duration) ([]router.MXHost, error)
	deliverHostFn func(context.Context, string, int, *model.Message, string, bool) error
	spoolWriteFn  func(*model.Message, string) error
}

func NewClient(cfg config.Config) *Client {
	c := &Client{
		cfg:         cfg,
		dane:        NewDANEResolver(cfg.DialTimeout, nil),
		mtaSTS:      NewMTASTSResolver(cfg.MTASTSCacheTTL, cfg.MTASTSFetchTimeout, nil),
		resolveMXFn: router.LookupWithTimeout,
	}
	if cfg.DKIMSignDomain != "" || cfg.DKIMSignSelector != "" || cfg.DKIMPrivateKeyFile != "" {
		if signer, err := dkim.NewFileSigner(cfg.DKIMSignDomain, cfg.DKIMSignSelector, cfg.DKIMPrivateKeyFile, cfg.DKIMSignHeaders); err == nil {
			c.signer = signer
		}
	}
	c.deliverHostFn = c.deliverHost
	c.spoolWriteFn = c.writeLocalSpool
	return c
}

func (c *Client) Deliver(ctx context.Context, msg *model.Message, rcpt string) error {
	mode := strings.ToLower(strings.TrimSpace(c.cfg.DeliveryMode))
	if mode == "" {
		mode = "mx"
	}
	switch mode {
	case "mx":
		return c.deliverByMX(ctx, msg, rcpt)
	case "local_spool":
		return c.spoolWriteFn(msg, rcpt)
	case "relay":
		if strings.TrimSpace(c.cfg.RelayHost) == "" {
			return errors.New("relay mode requires MTA_RELAY_HOST")
		}
		port := c.cfg.RelayPort
		if port <= 0 {
			port = 25
		}
		return c.deliverHostFn(ctx, c.cfg.RelayHost, port, msg, rcpt, c.cfg.RelayRequireTLS)
	default:
		return fmt.Errorf("unknown delivery mode: %s", mode)
	}
}

func (c *Client) deliverByMX(ctx context.Context, msg *model.Message, rcpt string) error {
	domain, ok := util.DomainOf(rcpt)
	if !ok {
		return errors.New("invalid rcpt domain")
	}
	mxHosts, err := c.resolveMXFn(domain, c.cfg.DialTimeout)
	if err != nil {
		return fmt.Errorf("mx lookup failed: %w", err)
	}
	requireTLS := false
	daneCandidates := make([]router.MXHost, 0, len(mxHosts))
	if c.dane != nil {
		for _, mx := range mxHosts {
			res, lErr := c.dane.LookupHost(ctx, mx.Host, 25)
			if lErr != nil {
				continue
			}
			if res.HasUsableTLSA() {
				daneCandidates = append(daneCandidates, mx)
			}
		}
	}
	if len(daneCandidates) > 0 {
		// RFC 7672 precedence: if usable DANE is available, apply it before MTA-STS.
		requireTLS = true
		mxHosts = daneCandidates
	} else if c.mtaSTS != nil {
		if p, pErr := c.mtaSTS.Lookup(ctx, domain); pErr == nil {
			if p.Mode == "enforce" {
				requireTLS = true
				filtered := make([]router.MXHost, 0, len(mxHosts))
				for _, mx := range mxHosts {
					if p.AllowsMX(mx.Host) {
						filtered = append(filtered, mx)
					}
				}
				if len(filtered) == 0 {
					return &SMTPResponseError{Code: 454, Line: "mta-sts policy mismatch: no allowed mx"}
				}
				mxHosts = filtered
			}
		}
	}
	var lastErr error
	for _, mx := range mxHosts {
		if err := c.deliverHostFn(ctx, mx.Host, 25, msg, rcpt, requireTLS); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no mx targets")
	}
	return lastErr
}

func (c *Client) deliverHost(ctx context.Context, host string, port int, msg *model.Message, rcpt string, requireTLS bool) error {
	if port <= 0 {
		port = 25
	}
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.SendTimeout))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if _, _, err := readCode(r); err != nil {
		return err
	}

	if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
		return err
	}
	ehloLines, err := readLines(r)
	if err != nil {
		return err
	}
	startTLS := false
	for _, l := range ehloLines {
		if strings.Contains(strings.ToUpper(l), "STARTTLS") {
			startTLS = true
			break
		}
	}

	if startTLS {
		if err := writeLine(w, "STARTTLS"); err != nil {
			return err
		}
		code, _, err := readCode(r)
		if err != nil {
			return err
		}
		if code == 220 {
			tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				if requireTLS {
					return &SMTPResponseError{Code: 454, Line: "starttls handshake failed"}
				}
				return err
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			if err := writeLine(w, "EHLO "+c.cfg.Hostname); err != nil {
				return err
			}
			if _, err := readLines(r); err != nil {
				return err
			}
		}
	} else if requireTLS {
		return &SMTPResponseError{Code: 454, Line: "mta-sts enforce requires starttls"}
	}

	if err := writeLine(w, fmt.Sprintf("MAIL FROM:<%s>", msg.MailFrom)); err != nil {
		return err
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("mail from rejected: %w", err)
	}

	if err := writeLine(w, fmt.Sprintf("RCPT TO:<%s>", rcpt)); err != nil {
		return err
	}
	if err := expect2xx3xx(r); err != nil {
		return fmt.Errorf("rcpt rejected: %w", err)
	}

	if err := writeLine(w, "DATA"); err != nil {
		return err
	}
	if err := expectCode(r, 354); err != nil {
		return fmt.Errorf("data not accepted: %w", err)
	}

	payload, err := c.prepareOutboundData(msg.Data)
	if err != nil {
		return err
	}
	data := dotStuff(payload)
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.WriteString("\r\n.\r\n"); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := expect2xx(r); err != nil {
		return fmt.Errorf("final delivery reject: %w", err)
	}
	_ = writeLine(w, "QUIT")
	_, _, _ = readCode(r)
	return nil
}

func (c *Client) writeLocalSpool(msg *model.Message, rcpt string) error {
	dir := strings.TrimSpace(c.cfg.LocalSpoolDir)
	if dir == "" {
		dir = "./var/spool"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	name := fmt.Sprintf("%s_%s.eml", id, sanitizeFilename(rcpt))
	path := filepath.Join(dir, name)
	payload, err := c.prepareOutboundData(msg.Data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func sanitizeFilename(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func writeLine(w *bufio.Writer, line string) error {
	if _, err := w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return w.Flush()
}

func readCode(r *bufio.Reader) (int, string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, line, errors.New("short smtp response")
	}
	var code int
	_, err = fmt.Sscanf(line[:3], "%d", &code)
	if err != nil {
		return 0, line, err
	}
	return code, line, nil
}

func readLines(r *bufio.Reader) ([]string, error) {
	var out []string
	for {
		code, line, err := readCode(r)
		if err != nil {
			return nil, err
		}
		if code < 200 || code > 399 {
			return nil, &SMTPResponseError{Code: code, Line: line}
		}
		out = append(out, line)
		if len(line) >= 4 && line[3] == ' ' {
			return out, nil
		}
	}
}

func expect2xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func expect2xx3xx(r *bufio.Reader) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if (code < 200 || code > 299) && (code < 300 || code > 399) {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func expectCode(r *bufio.Reader, expected int) error {
	code, line, err := readCode(r)
	if err != nil {
		return err
	}
	if code != expected {
		return &SMTPResponseError{Code: code, Line: line}
	}
	return nil
}

func dotStuff(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
		if strings.HasPrefix(lines[i], ".") {
			lines[i] = "." + lines[i]
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}

func (c *Client) prepareOutboundData(raw []byte) ([]byte, error) {
	if c.signer == nil {
		return raw, nil
	}
	return c.signer.Sign(raw)
}
