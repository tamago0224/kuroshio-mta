package smtp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

type addressedConn struct {
	net.Conn
	addr net.Addr
}

func (c addressedConn) RemoteAddr() net.Addr { return c.addr }

func proxyFrame(ip string) []byte {
	src := net.ParseIP(ip)
	dst := net.ParseIP("203.0.113.2")
	family := byte(0x11)
	if src.To4() != nil {
		src = src.To4()
		dst = dst.To4()
	} else {
		src = src.To16()
		dst = net.ParseIP("2001:db8::2").To16()
		family = 0x21
	}
	b := append([]byte(nil), proxyV2Signature...)
	b = append(b, 0x21, family, 0, byte(len(src)*2+4))
	b = append(b, src...)
	b = append(b, dst...)
	return append(b, 0x30, 0x39, 0, 25)
}

func TestProxyV2Frame(t *testing.T) {
	for _, ip := range []string{"198.51.100.42", "2001:db8::42"} {
		t.Run(ip, func(t *testing.T) {
			p, err := newProxyPolicy(config.ProxyProtocolConfig{SMTP: true, TrustedCIDRs: []string{"127.0.0.1/32", "::1/128"}})
			if err != nil {
				t.Fatal(err)
			}
			peerIP := "127.0.0.1"
			if net.ParseIP(ip).To4() == nil {
				peerIP = "::1"
			}
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			client.SetDeadline(time.Now().Add(5 * time.Second))
			packet := append(proxyFrame(ip), []byte("EHLO example.test\r\n")...)
			go client.Write(packet)
			conn, reason := p.accept(context.Background(), addressedConn{server, &net.TCPAddr{IP: net.ParseIP(peerIP), Port: 1111}})
			if reason != "" {
				t.Fatal(reason)
			}
			if conn.RemoteAddr().String() != net.JoinHostPort(ip, "12345") {
				t.Fatal(conn.RemoteAddr())
			}
			if conn.(*proxyConn).peer.String() != net.JoinHostPort(peerIP, "1111") {
				t.Fatal("transport peer lost")
			}
			got := make([]byte, len("EHLO example.test\r\n"))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Fatal(err)
			}
			if string(got) != "EHLO example.test\r\n" {
				t.Fatalf("overread: %q", got)
			}
		})
	}
}

func TestProxyV2InvalidFrames(t *testing.T) {
	for _, name := range []string{"untrusted", "signature", "version", "command", "oversized", "truncated", "short address", "UDP", "bad TLV", "SSL TLV", "LOCAL"} {
		t.Run(name, func(t *testing.T) {
			p, _ := newProxyPolicy(config.ProxyProtocolConfig{SMTP: true, TrustedCIDRs: []string{"127.0.0.1/32"}})
			frame := proxyFrame("198.51.100.42")
			peer := "127.0.0.1"
			want := "invalid"
			switch name {
			case "untrusted":
				peer = "192.0.2.1"
				want = "untrusted"
			case "signature":
				frame[0] = 0
			case "version":
				frame[12] = 0x11
			case "command":
				frame[12] = 0x22
			case "oversized":
				binary.BigEndian.PutUint16(frame[14:16], proxyMaxPayload+1)
				want = "oversized"
			case "truncated":
				frame = frame[:20]
				want = "read"
			case "short address":
				frame = frame[:20]
				frame[15] = 4
			case "UDP":
				frame[13] = 0x12
			case "bad TLV":
				frame = append(frame, 0x20, 0, 10)
				frame[15] += 3
			case "SSL TLV":
				frame = append(frame, 0x20, 0, 5, 1, 0, 0, 0, 0)
				frame[15] += 8
				want = ""
			case "LOCAL":
				frame[12] = 0x20
				frame[13] = 0xff
				want = ""
			}
			client, server := net.Pipe()
			defer server.Close()
			go func() { client.Write(frame); client.Close() }()
			conn, reason := p.accept(context.Background(), addressedConn{server, &net.TCPAddr{IP: net.ParseIP(peer), Port: 123}})
			if reason != want {
				t.Fatalf("reason=%q want=%q", reason, want)
			}
			if name == "LOCAL" && conn.RemoteAddr().String() != "127.0.0.1:123" {
				t.Fatal("LOCAL replaced peer")
			}
		})
	}
}

func TestProxyV2TimeoutAndCancellation(t *testing.T) {
	for _, name := range []string{"timeout", "cancellation"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, server := net.Pipe()
				defer client.Close()
				defer server.Close()
				p, _ := newProxyPolicy(config.ProxyProtocolConfig{TrustedCIDRs: []string{"127.0.0.1/32"}})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan string, 1)
				go func() {
					_, reason := p.accept(ctx, addressedConn{server, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}})
					done <- reason
				}()
				synctest.Wait()
				if name == "timeout" {
					time.Sleep(6 * time.Second)
				} else {
					cancel()
				}
				if reason := <-done; reason != "read" {
					t.Fatal(reason)
				}
			})
		})
	}
}

func TestProxyListenerSelectionAndPlaintextAUTH(t *testing.T) {
	for _, name := range []string{"SMTP", "Submission", "implicit TLS", "disabled"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{Hostname: "mx.test", InboundPolicy: config.InboundPolicyConfig{TrustedCIDRs: []string{"127.0.0.1/32"}}, ProxyProtocol: config.ProxyProtocolConfig{SMTP: true, Submission: true, SubmissionTLS: true, TrustedCIDRs: []string{"127.0.0.1/32"}}}
			var s *Server
			switch name {
			case "SMTP":
				s = NewServer(cfg, nil, observability.NewMetrics())
			case "Submission":
				s = NewSubmissionServer(cfg, nil, observability.NewMetrics(), fakeUserAuthBackend{ok: true})
			case "implicit TLS":
				s = NewImplicitTLSSubmissionServer(cfg, nil, observability.NewMetrics(), nil)
			case "disabled":
				cfg.ProxyProtocol = config.ProxyProtocolConfig{}
				s = NewServer(cfg, nil, nil)
			}
			if s.proxyEnabled() != (name != "disabled") {
				t.Fatal("wrong listener selection")
			}
			if name == "implicit TLS" {
				return
			} // TLS byte ordering is also covered by the loopback LB test.
			client, server := net.Pipe()
			defer client.Close()
			client.SetDeadline(time.Now().Add(5 * time.Second))
			go s.handleConn(addressedConn{server, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 40000}})
			if name != "disabled" {
				frame := proxyFrame("198.51.100.42")
				frame = append(frame, 0x20, 0, 5, 1, 0, 0, 0, 0)
				frame[15] += 8
				if _, err := client.Write(frame); err != nil {
					t.Fatal(err)
				}
			}
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			if name == "Submission" {
				mustWriteSMTPLine(t, w, "EHLO client")
				readSMTPResponse(t, r)
				mustWriteSMTPLine(t, w, "AUTH PLAIN invalid")
				if _, code := readSMTPResponse(t, r); code != 538 {
					t.Fatalf("SSL TLV bypassed TLS: %d", code)
				}
			}
			mustWriteSMTPLine(t, w, "NOOP")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "EHLO client")
			readSMTPResponse(t, r)
			mustWriteSMTPLine(t, w, "MAIL FROM:<sender@sender.test>")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "RCPT TO:<user@external.test>")
			want := 550
			if name == "disabled" {
				want = 250
			}
			if _, code := readSMTPResponse(t, r); code != want {
				t.Fatalf("relay policy code=%d want=%d", code, want)
			}
			if name != "disabled" && !bytes.Contains([]byte(s.metrics.RenderPrometheus()), []byte("smtp_proxy_accepted_total 1")) {
				t.Fatal("missing proxy metric")
			}
		})
	}
}
