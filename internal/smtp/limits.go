package smtp

import (
	"bufio"
	"errors"
	"net"
	"sync"
	"time"
)

var errSessionBytes = errors.New("session byte limit reached")

// limitedConn preserves the absolute lifetime across STARTTLS and refreshes the
// idle deadline on network I/O. Input accounting includes TLS and SMTP framing.
type limitedConn struct {
	net.Conn
	expires      time.Time
	idle         time.Duration
	remaining    int64
	deadlineMu   sync.Mutex
	hardDeadline time.Time
}

func (c *limitedConn) deadline() time.Time {
	c.deadlineMu.Lock()
	defer c.deadlineMu.Unlock()
	deadline := minTime(time.Now().Add(c.idle), c.expires)
	if !c.hardDeadline.IsZero() {
		deadline = minTime(deadline, c.hardDeadline)
	}
	return deadline
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (c *limitedConn) SetDeadline(t time.Time) error {
	if t.IsZero() || t.After(c.expires) {
		t = c.expires
	}
	c.deadlineMu.Lock()
	c.hardDeadline = t
	c.deadlineMu.Unlock()
	return c.Conn.SetDeadline(t)
}

func (c *limitedConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errSessionBytes
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	if err := c.Conn.SetReadDeadline(c.deadline()); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}

func (c *limitedConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(c.deadline()); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

// Empty key reserves a listener-wide slot before the handler goroutine starts.
// Nonempty keys reserve the effective client IP after trusted PROXY parsing.
func (s *Server) acquireConnection(ip string) bool {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	limits := s.cfg.SMTPLimits.WithDefaults()
	if ip == "" {
		if s.connections >= limits.MaxConnections {
			return false
		}
		s.connections++
		s.updateConnectionGauge()
		return true
	}
	if s.connectionsPerIP == nil {
		s.connectionsPerIP = map[string]int{}
	}
	if s.connectionsPerIP[ip] >= limits.MaxConnectionsPerIP {
		return false
	}
	s.connectionsPerIP[ip]++
	return true
}

func (s *Server) releaseConnection(ip string) {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if ip == "" {
		s.connections--
		s.updateConnectionGauge()
		return
	}
	s.connectionsPerIP[ip]--
	if s.connectionsPerIP[ip] == 0 {
		delete(s.connectionsPerIP, ip)
	}
}

// Caller holds connectionsMu; fixed listener names avoid per-IP cardinality.
func (s *Server) updateConnectionGauge() {
	if s.metrics == nil {
		return
	}
	listener := "smtp"
	if s.submission {
		listener = "submission"
	}
	if s.implicitTLS {
		listener = "submission_tls"
	}
	s.metrics.SetGauge("smtp_active_connections_"+listener, int64(s.connections))
	s.metrics.SetGauge("smtp_connection_limit_"+listener, int64(s.cfg.SMTPLimits.WithDefaults().MaxConnections))
}

// ReadSlice bounds allocation even when a sender never terminates a DATA line.
func readBoundedDataLine(r *bufio.Reader) (string, error) {
	line := make([]byte, 0, 1000)
	for {
		fragment, err := r.ReadSlice('\n')
		if len(line)+len(fragment) > 1000 {
			return "", errDataLineTooLong
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return string(line), err
	}
}
