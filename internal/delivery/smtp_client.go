package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/model"
	"github.com/tamago0224/orinoco-mta/internal/router"
	"github.com/tamago0224/orinoco-mta/internal/util"
)

type Client struct {
	cfg config.Config
}

func NewClient(cfg config.Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Deliver(ctx context.Context, msg *model.Message, rcpt string) error {
	domain, ok := util.DomainOf(rcpt)
	if !ok {
		return errors.New("invalid rcpt domain")
	}
	mxHosts, err := router.LookupWithTimeout(domain, c.cfg.DialTimeout)
	if err != nil {
		return fmt.Errorf("mx lookup failed: %w", err)
	}
	var lastErr error
	for _, mx := range mxHosts {
		if err := c.deliverHost(ctx, mx.Host, msg, rcpt); err == nil {
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

func (c *Client) deliverHost(ctx context.Context, host string, msg *model.Message, rcpt string) error {
	dialer := &net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "25"))
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

	data := dotStuff(msg.Data)
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
