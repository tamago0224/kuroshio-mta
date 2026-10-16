package smtp

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
)

func TestSTARTTLSHandshakeFailureClosesConnection(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:       config.Config{Hostname: "mx.example.test"},
		tlsConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleConn(server)
	}()
	defer func() {
		client.Close()
		server.Close()
		<-done
	}()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	r, w := bufio.NewReader(client), bufio.NewWriter(client)
	readSMTPResponse(t, r)
	mustWriteSMTPLine(t, w, "STARTTLS")
	if _, code := readSMTPResponse(t, r); code != 220 {
		t.Fatalf("STARTTLS code = %d, want 220", code)
	}
	// Five non-TLS bytes are enough to reject the record header without waiting
	// for additional input. The server must close rather than resume SMTP.
	if _, err := client.Write([]byte("NOOP!")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatalf("read after failed handshake = %v, want EOF", err)
	}
}
