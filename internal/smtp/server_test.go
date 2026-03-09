package smtp

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
)

func TestSplitVerb(t *testing.T) {
	verb, arg := splitVerb("MAIL FROM:<a@example.com>")
	if verb != "MAIL" || arg != "FROM:<a@example.com>" {
		t.Fatalf("verb=%q arg=%q", verb, arg)
	}
}

func TestParseMailFrom(t *testing.T) {
	got, err := parseMailFrom("FROM:<Alice@Example.com>")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Address != "alice@example.com" {
		t.Fatalf("got=%q", got.Address)
	}
	if _, err := parseMailFrom("TO:<alice@example.com>"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseMailFromWithParameters(t *testing.T) {
	got, err := parseMailFrom("FROM:<Alice@Example.com> SIZE=123 BODY=8BITMIME")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Address != "alice@example.com" {
		t.Fatalf("address=%q", got.Address)
	}
	if got.Size != 123 {
		t.Fatalf("size=%d", got.Size)
	}
	if got.Body != "8BITMIME" {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestParseMailFromRejectsUnknownParameter(t *testing.T) {
	if _, err := parseMailFrom("FROM:<alice@example.com> FOO=bar"); err == nil {
		t.Fatal("expected unknown param error")
	}
}

func TestParseRcptTo(t *testing.T) {
	got, err := parseRcptTo("TO:<Bob@Example.com>", "mx.example.test")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "bob@example.com" {
		t.Fatalf("got=%q", got)
	}
	if _, err := parseRcptTo("TO:<>", "mx.example.test"); err == nil {
		t.Fatal("expected error for empty rcpt")
	}
}

func TestParseRcptToPostmasterWithoutDomain(t *testing.T) {
	got, err := parseRcptTo("TO:<postmaster>", "mx.example.test")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "postmaster@mx.example.test" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadData(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("hello\r\n..escaped\r\n.\r\n"))
	data, err := readData(in, 1024)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "hello\r\n.escaped\r\n"
	if string(data) != want {
		t.Fatalf("got=%q want=%q", string(data), want)
	}
}

func TestReadDataTooLarge(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("hello\r\nworld\r\n.\r\n"))
	_, err := readData(in, 5)
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestParseRemoteIP(t *testing.T) {
	if got := parseRemoteIP("127.0.0.1:25"); got == nil || got.String() != "127.0.0.1" {
		t.Fatalf("got=%v", got)
	}
	if got := parseRemoteIP("2001:db8::1"); got == nil || got.String() != "2001:db8::1" {
		t.Fatalf("got=%v", got)
	}
}

func TestEHLOResponseWithoutTLSDoesNotAdvertiseStartTLS(t *testing.T) {
	s := &Server{cfg: config.Config{Hostname: "mx.example.test"}}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)

	_, _ = readSMTPResponse(t, r) // banner
	if _, err := w.WriteString("EHLO client.example\r\n"); err != nil {
		t.Fatalf("write EHLO: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush EHLO: %v", err)
	}
	resp, _ := readSMTPResponse(t, r)
	if strings.Contains(resp, "STARTTLS") {
		t.Fatalf("STARTTLS must not be advertised when TLS is not configured: %q", resp)
	}
}

func TestSTARTTLSWithoutTLSConfigReturns454(t *testing.T) {
	s := &Server{cfg: config.Config{Hostname: "mx.example.test"}}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)

	_, _ = readSMTPResponse(t, r) // banner
	if _, err := w.WriteString("EHLO client.example\r\n"); err != nil {
		t.Fatalf("write EHLO: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush EHLO: %v", err)
	}
	_, _ = readSMTPResponse(t, r)

	if _, err := w.WriteString("STARTTLS\r\n"); err != nil {
		t.Fatalf("write STARTTLS: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush STARTTLS: %v", err)
	}
	resp, code := readSMTPResponse(t, r)
	if code != 454 {
		t.Fatalf("code=%d resp=%q want=454", code, resp)
	}
}

func TestSTARTTLSWithTLSConfigUpgradesConnection(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	s := &Server{
		cfg:       config.Config{Hostname: "mx.example.test"},
		tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r) // banner

	if _, err := w.WriteString("EHLO client.example\r\n"); err != nil {
		t.Fatalf("write EHLO: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush EHLO: %v", err)
	}
	resp, _ := readSMTPResponse(t, r)
	if !strings.Contains(resp, "STARTTLS") {
		t.Fatalf("STARTTLS must be advertised when TLS is configured: %q", resp)
	}

	if _, err := w.WriteString("STARTTLS\r\n"); err != nil {
		t.Fatalf("write STARTTLS: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush STARTTLS: %v", err)
	}
	_, code := readSMTPResponse(t, r)
	if code != 220 {
		t.Fatalf("code=%d want=220", code)
	}

	tlsClient := tls.Client(client, &tls.Config{InsecureSkipVerify: true})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("tls handshake: %v", err)
	}
	defer tlsClient.Close()
	rt := bufio.NewReader(tlsClient)
	wt := bufio.NewWriter(tlsClient)

	if _, err := wt.WriteString("EHLO client.example\r\n"); err != nil {
		t.Fatalf("write EHLO over TLS: %v", err)
	}
	if err := wt.Flush(); err != nil {
		t.Fatalf("flush EHLO over TLS: %v", err)
	}
	_, code = readSMTPResponse(t, rt)
	if code != 250 {
		t.Fatalf("code=%d want=250", code)
	}
}

func TestHELPVRFYEXPNCommands(t *testing.T) {
	s := &Server{cfg: config.Config{Hostname: "mx.example.test"}}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go s.handleConn(server)

	r := bufio.NewReader(client)
	w := bufio.NewWriter(client)
	_, _ = readSMTPResponse(t, r) // banner

	if _, err := w.WriteString("HELP\r\n"); err != nil {
		t.Fatalf("write HELP: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush HELP: %v", err)
	}
	helpResp, helpCode := readSMTPResponse(t, r)
	if helpCode != 214 || !strings.Contains(helpResp, "Supported") {
		t.Fatalf("unexpected HELP response: %d %q", helpCode, helpResp)
	}

	if _, err := w.WriteString("VRFY user@example.test\r\n"); err != nil {
		t.Fatalf("write VRFY: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush VRFY: %v", err)
	}
	_, vrfyCode := readSMTPResponse(t, r)
	if vrfyCode != 252 {
		t.Fatalf("vrfy code=%d want=252", vrfyCode)
	}

	if _, err := w.WriteString("EXPN staff\r\n"); err != nil {
		t.Fatalf("write EXPN: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush EXPN: %v", err)
	}
	_, expnCode := readSMTPResponse(t, r)
	if expnCode != 502 {
		t.Fatalf("expn code=%d want=502", expnCode)
	}
}

func readSMTPResponse(t *testing.T, r *bufio.Reader) (string, int) {
	t.Helper()
	var lines []string
	code := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		lines = append(lines, line)
		if len(line) >= 3 && code == 0 {
			if _, err := fmt.Sscanf(line[:3], "%d", &code); err != nil {
				t.Fatalf("parse code: %v line=%q", err, line)
			}
		}
		if len(line) >= 4 && line[3] == ' ' {
			return strings.Join(lines, "\n"), code
		}
	}
}

func selfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mx.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"mx.example.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        tmpl,
	}, nil
}
