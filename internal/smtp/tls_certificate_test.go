package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

func writeTLSFiles(t *testing.T, cfg config.Config, cert tls.Certificate) {
	t.Helper()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		cfg.TLSCertFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		cfg.TLSKeyFile:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTLSCertificateRejectsInvalidUpdates(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	other, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"malformed", "partial", "missing", "mismatched key", "expired", "not yet valid"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{TLSCertFile: filepath.Join(dir, "cert.pem"), TLSKeyFile: filepath.Join(dir, "key.pem")}
			writeTLSFiles(t, cfg, cert)
			manager, conf, err := loadTLSConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			old, _ := conf.GetCertificate(nil)
			now := time.Now()
			switch name {
			case "malformed":
				err = os.WriteFile(cfg.TLSCertFile, []byte("invalid"), 0600)
			case "partial":
				err = os.WriteFile(cfg.TLSCertFile, []byte("-----BEGIN CERTIFICATE-----\nMI"), 0600)
			case "missing":
				err = os.Remove(cfg.TLSKeyFile)
			case "mismatched key":
				writeTLSFiles(t, cfg, other)
				err = os.WriteFile(cfg.TLSCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600)
			case "expired":
				now = cert.Leaf.NotAfter
			case "not yet valid":
				now = cert.Leaf.NotBefore.Add(-time.Second)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.reload(now); err == nil {
				t.Fatal("invalid update accepted")
			}
			got, _ := conf.GetCertificate(nil)
			if got != old {
				t.Fatal("last valid certificate replaced")
			}
		})
	}
}

func TestTLSCertificateUpdatesAcrossListeners(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Hostname: "mx.example.test", TLSCertFile: filepath.Join(dir, "cert.pem"), TLSKeyFile: filepath.Join(dir, "key.pem")}
	first, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	second, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	writeTLSFiles(t, cfg, first)
	roots := x509.NewCertPool()
	for _, cert := range []tls.Certificate{first, second} {
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		roots.AddCert(leaf)
	}
	servers := []*Server{NewServer(cfg, nil, nil), NewSubmissionServer(cfg, nil, nil, nil), NewImplicitTLSSubmissionServer(cfg, nil, nil, nil)}
	connect := func(s *Server, want tls.Certificate) (*bufio.Reader, *bufio.Writer) {
		t.Helper()
		client, server := net.Pipe()
		t.Cleanup(func() { client.Close() })
		client.SetDeadline(time.Now().Add(10 * time.Second))
		go s.handleConn(server)
		if !s.implicitTLS {
			r, w := bufio.NewReader(client), bufio.NewWriter(client)
			readSMTPResponse(t, r)
			mustWriteSMTPLine(t, w, "EHLO client")
			readSMTPResponse(t, r)
			mustWriteSMTPLine(t, w, "STARTTLS")
			if _, code := readSMTPResponse(t, r); code != 220 {
				t.Fatalf("STARTTLS=%d", code)
			}
		}
		c := tls.Client(client, &tls.Config{RootCAs: roots, ServerName: cfg.Hostname, MinVersion: tls.VersionTLS12})
		if err := c.Handshake(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(c.ConnectionState().PeerCertificates[0].Raw, want.Certificate[0]) {
			t.Fatal("unexpected peer certificate")
		}
		r, w := bufio.NewReader(c), bufio.NewWriter(c)
		if s.implicitTLS {
			readSMTPResponse(t, r)
		}
		return r, w
	}
	type sessionIO struct {
		r *bufio.Reader
		w *bufio.Writer
	}
	var existing []sessionIO
	for _, s := range servers {
		r, w := connect(s, first)
		existing = append(existing, sessionIO{r, w})
	}
	writeTLSFiles(t, cfg, second)
	for _, s := range servers {
		if changed, err := s.certificate.reload(time.Now()); err != nil || !changed {
			t.Fatalf("reload=%v,%v", changed, err)
		}
		connect(s, second)
	}
	for _, io := range existing {
		mustWriteSMTPLine(t, io.w, "NOOP")
		if _, code := readSMTPResponse(t, io.r); code != 250 {
			t.Fatalf("existing session NOOP=%d", code)
		}
	}
}

func TestTLSCertificateWatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cert, err := selfSignedCert()
		if err != nil {
			t.Fatal(err)
		}
		next, err := selfSignedCert()
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		cfg := config.Config{TLSCertFile: filepath.Join(dir, "cert.pem"), TLSKeyFile: filepath.Join(dir, "key.pem")}
		writeTLSFiles(t, cfg, cert)
		s := NewServer(cfg, nil, observability.NewMetrics())
		if s.tlsLoadErr != nil {
			t.Fatal(s.tlsLoadErr)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); s.watchCertificate(ctx) }()
		synctest.Wait()
		if err := os.WriteFile(cfg.TLSKeyFile, []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(61 * time.Second)
		synctest.Wait()
		rendered := s.metrics.RenderPrometheus()
		if !strings.Contains(rendered, "smtp_tls_certificate_reload_error_total 1") || !strings.Contains(rendered, "smtp_tls_certificate_not_after_seconds ") {
			t.Fatal(rendered)
		}
		writeTLSFiles(t, cfg, next)
		time.Sleep(time.Minute)
		synctest.Wait()
		got, _ := s.tlsConfig.GetCertificate(nil)
		if !bytes.Equal(got.Certificate[0], next.Certificate[0]) {
			t.Fatal("watcher did not publish update")
		}
		if !strings.Contains(s.metrics.RenderPrometheus(), "smtp_tls_certificate_reload_total 1") {
			t.Fatal("missing reload metric")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if s.metrics.Counter("smtp_tls_certificate_reload_total").Load() != 1 {
			t.Fatal("unchanged certificate counted as update")
		}
		cancel()
		<-done
	})
}
