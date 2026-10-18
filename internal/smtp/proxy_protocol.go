package smtp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

const proxyMaxPayload = 4096

var proxyV2Signature = []byte("\r\n\r\n\x00\r\nQUIT\n")

type proxyPolicy struct{ trusted []*net.IPNet }

func newProxyPolicy(cfg config.ProxyProtocolConfig) (*proxyPolicy, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p := &proxyPolicy{}
	for _, cidr := range cfg.TrustedCIDRs {
		_, network, _ := net.ParseCIDR(cidr)
		p.trusted = append(p.trusted, network)
	}
	return p, nil
}

func (s *Server) proxyEnabled() bool {
	if s.implicitTLS {
		return s.cfg.ProxyProtocol.SubmissionTLS
	}
	if s.submission {
		return s.cfg.ProxyProtocol.Submission
	}
	return s.cfg.ProxyProtocol.SMTP
}

// peer remains the transport endpoint; RemoteAddr consistently exposes the client
// to SMTP authorization, SPF, DNSBL, rate limiting, hooks, and Received headers.
type proxyConn struct {
	net.Conn
	peer   net.Addr
	client *net.TCPAddr
}

func (c *proxyConn) RemoteAddr() net.Addr { return c.client }

func (p *proxyPolicy) accept(ctx context.Context, conn net.Conn) (net.Conn, string) {
	peer := parseRemoteIP(conn.RemoteAddr().String())
	trusted := false
	for _, network := range p.trusted {
		if network.Contains(peer) {
			trusted = true
			break
		}
	}
	if !trusted {
		return nil, "untrusted"
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, "read"
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	// ReadFull consumes exactly the header, preserving coalesced SMTP/TLS bytes.
	var header [16]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, "read"
	}
	if !bytes.Equal(header[:12], proxyV2Signature) || (header[12] != 0x20 && header[12] != 0x21) {
		return nil, "invalid"
	}
	n := int(binary.BigEndian.Uint16(header[14:16]))
	if n > proxyMaxPayload {
		return nil, "oversized"
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, "read"
	}
	_ = conn.SetReadDeadline(time.Time{})
	// LOCAL represents a proxy's own connection; ignore address and TLV fields.
	if header[12] == 0x20 {
		return conn, ""
	}
	var width int
	switch header[13] {
	case 0x11:
		width = 4
	case 0x21:
		width = 16
	default:
		return nil, "invalid" // Only TCP IPv4/IPv6 client identities are supported.
	}
	base := 2*width + 4
	if len(payload) < base {
		return nil, "invalid"
	}
	// Skip unknown TLVs, but require complete length-delimited vectors. SSL TLVs
	// never change the session TLS state or permit plaintext SMTP AUTH.
	for extra := payload[base:]; len(extra) > 0; {
		if len(extra) < 3 {
			return nil, "invalid"
		}
		size := int(binary.BigEndian.Uint16(extra[1:3]))
		if size > len(extra)-3 {
			return nil, "invalid"
		}
		extra = extra[3+size:]
	}
	client := &net.TCPAddr{IP: append(net.IP(nil), payload[:width]...), Port: int(binary.BigEndian.Uint16(payload[2*width : base-2]))}
	if client.IP.IsUnspecified() || client.IP.IsMulticast() {
		return nil, "invalid"
	}
	return &proxyConn{Conn: conn, peer: conn.RemoteAddr(), client: client}, ""
}
