package delivery

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestRelayAuthenticationFailureAfterVerifiedTLS(t *testing.T) {
	certificate, roots := relayTestCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverResult := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 ready\r\n")
		if line, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			serverResult <- fmt.Errorf("initial EHLO: %q %v", line, err)
			return
		}
		fmt.Fprint(conn, "250-localhost\r\n250 STARTTLS\r\n")
		if line, err := r.ReadString('\n'); err != nil || line != "STARTTLS\r\n" {
			serverResult <- fmt.Errorf("STARTTLS: %q %v", line, err)
			return
		}
		fmt.Fprint(conn, "220 go ahead\r\n")
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			serverResult <- err
			return
		}
		r = bufio.NewReader(tlsConn)
		if line, err := r.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			serverResult <- fmt.Errorf("TLS EHLO: %q %v", line, err)
			return
		}
		fmt.Fprint(tlsConn, "250-localhost\r\n250 AUTH PLAIN\r\n")
		line, err := r.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "AUTH PLAIN ") {
			serverResult <- fmt.Errorf("AUTH: %q %v", line, err)
			return
		}
		fmt.Fprint(tlsConn, "535 5.7.8 invalid credentials\r\n")
		serverResult <- nil
	}()

	client := NewClient(config.Config{Hostname: "mta.example.test", DialTimeout: time.Second, SendTimeout: 3 * time.Second})
	defer client.Close()
	client.tlsConfigFn = func(host string) *tls.Config {
		return &tls.Config{ServerName: host, RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	ctx := context.WithValue(context.Background(), relayAuthContextKey{}, relayAuth{username: "sender", password: "secret"})
	observation := &AttemptObservation{}
	ctx = WithAttemptObservation(ctx, observation)
	port := listener.Addr().(*net.TCPAddr).Port
	err = client.deliverHost(ctx, "localhost", port, &model.Message{}, "recipient@example.test", true, nil)
	var response *SMTPResponseError
	if !errors.As(err, &response) || response.Stage != StageAUTH || response.Code != 535 {
		t.Fatalf("authentication error = %v", err)
	}
	if !observation.TLSApplied {
		t.Fatal("verified STARTTLS was not observed")
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func relayTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return certificate, roots
}
