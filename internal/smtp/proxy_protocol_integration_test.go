//go:build integration

package smtp

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/ingress"
	"github.com/tamago0224/kuroshio-mta/internal/mailauth"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

func TestProxyV2LoopbackLB(t *testing.T) {
	original := evaluateAuthWithPolicy
	defer func() { evaluateAuthWithPolicy = original }()
	for _, name := range []string{"SMTP", "STARTTLS Submission", "implicit TLS Submission"} {
		t.Run(name, func(t *testing.T) {
			const clientIP = "198.51.100.42"
			authIP := make(chan string, 1)
			evaluateAuthWithPolicy = func(ip net.IP, _, _ string, _ []byte, _ mailauth.SPFPolicy) mailauth.Result {
				authIP <- ip.String()
				return mailauth.Result{Action: mailauth.ActionAccept}
			}
			cfg := config.Config{Hostname: "mx.example.test", MaxMessageBytes: 1 << 20, IngressRateLimit: 1,
				InboundPolicy: config.InboundPolicyConfig{LocalDomains: []string{"local.test"}, LocalRecipients: []string{"user@local.test"}},
				ProxyProtocol: config.ProxyProtocolConfig{SMTP: true, Submission: true, SubmissionTLS: true, TrustedCIDRs: []string{"127.0.0.1/32"}}}
			q := &recordingQueue{}
			var s *Server
			switch name {
			case "SMTP":
				s = NewServer(cfg, q, observability.NewMetrics())
			case "STARTTLS Submission":
				s = NewSubmissionServer(cfg, q, observability.NewMetrics(), nil)
			default:
				s = NewImplicitTLSSubmissionServer(cfg, q, observability.NewMetrics(), nil)
			}
			cert, err := selfSignedCert()
			if err != nil {
				t.Fatal(err)
			}
			s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
			dnsIP := make(chan string, 1)
			s.dnsbl = ingress.NewDNSBLChecker([]string{"dnsbl.test"}, time.Minute, func(host string) ([]string, error) { dnsIP <- host; return nil, nil })
			backend, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			lb, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer lb.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, err := backend.Accept()
				if err == nil {
					s.handleConn(c)
				}
			}()
			proxyDone := make(chan struct{})
			go func() {
				defer close(proxyDone)
				down, err := lb.Accept()
				if err != nil {
					return
				}
				defer down.Close()
				up, err := net.Dial("tcp", backend.Addr().String())
				if err != nil {
					return
				}
				defer up.Close()
				if _, err := up.Write(proxyFrame(clientIP)); err != nil {
					return
				}
				copied := make(chan struct{})
				go func() { io.Copy(up, down); up.(*net.TCPConn).CloseWrite(); close(copied) }()
				io.Copy(down, up)
				down.Close()
				<-copied
			}()
			c, err := net.Dial("tcp", lb.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Second))
			var conn net.Conn = c
			if !s.implicitTLS {
				r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
				readSMTPResponse(t, r)
				if s.submission {
					mustWriteSMTPLine(t, w, "EHLO client")
					readSMTPResponse(t, r)
					mustWriteSMTPLine(t, w, "STARTTLS")
					if _, code := readSMTPResponse(t, r); code != 220 {
						t.Fatal(code)
					}
				}
			}
			if s.submission {
				roots := x509.NewCertPool()
				leaf, err := x509.ParseCertificate(cert.Certificate[0])
				if err != nil {
					t.Fatal(err)
				}
				roots.AddCert(leaf)
				tc := tls.Client(c, &tls.Config{RootCAs: roots, ServerName: cfg.Hostname, MinVersion: tls.VersionTLS12})
				if err := tc.Handshake(); err != nil {
					t.Fatal(err)
				}
				conn = tc
			}
			r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
			if s.implicitTLS {
				readSMTPResponse(t, r)
			}
			for _, cmd := range []string{"EHLO client", "MAIL FROM:<sender@sender.test>", "RCPT TO:<user@local.test>"} {
				mustWriteSMTPLine(t, w, cmd)
				if _, code := readSMTPResponse(t, r); code != 250 {
					t.Fatalf("%s=%d", cmd, code)
				}
			}
			mustWriteSMTPLine(t, w, "DATA")
			if _, code := readSMTPResponse(t, r); code != 354 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "From: sender@sender.test\r\nTo: user@local.test\r\nSubject: proxy\r\n\r\nhello\r\n.")
			if _, code := readSMTPResponse(t, r); code != 250 {
				t.Fatal(code)
			}
			mustWriteSMTPLine(t, w, "QUIT")
			readSMTPResponse(t, r)
			c.Close()
			<-done
			<-proxyDone
			if got := <-authIP; got != clientIP {
				t.Fatalf("SPF IP=%s", got)
			}
			if got := <-dnsIP; got != "42.100.51.198.dnsbl.test" {
				t.Fatalf("DNSBL=%s", got)
			}
			if len(q.msgs) != 1 || !strings.Contains(string(q.msgs[0].Data), clientIP) {
				t.Fatal("Received missing forwarded IP")
			}
			// The forwarded client, rather than the TCP proxy, consumed the rate bucket.
			if allowed, _ := s.limiter.Allow(clientIP, time.Now().UTC()); allowed {
				t.Fatal("forwarded client did not consume rate limit")
			}
			if allowed, _ := s.limiter.Allow("127.0.0.1", time.Now().UTC()); !allowed {
				t.Fatal("proxy consumed client rate bucket")
			}
		})
	}
}
