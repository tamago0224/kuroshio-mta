package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

func TestDeliverLocalSpoolWritesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "local", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m1", MailFrom: "sender@example.com", Data: []byte("Subject: hi\r\n\r\nhello")}

	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver local spool: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected spool file")
	}
	p := filepath.Join(dir, entries[0].Name())
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("spool file is empty")
	}
}

func TestDeliverRelayUsesConfiguredTarget(t *testing.T) {
	cfg := config.Config{DeliveryMode: "relay", RelayHost: "relay.example.net", RelayPort: 2525, RelayRequireTLS: true}
	cl := NewClient(cfg)
	called := false
	cl.deliverHostFn = func(ctx context.Context, host string, port int, msg *model.Message, rcpt string, requireTLS bool, _ *DANEResult) error {
		called = true
		if host != "relay.example.net" || port != 2525 {
			t.Fatalf("unexpected relay target host=%s port=%d", host, port)
		}
		if !requireTLS {
			t.Fatal("relay require tls must be true")
		}
		return nil
	}
	msg := &model.Message{ID: "m2", MailFrom: "sender@example.com", Data: []byte("x")}
	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver relay: %v", err)
	}
	if !called {
		t.Fatal("relay deliverHostFn should be called")
	}
}

func TestDeliverLocalSpoolAppliesDKIMSignerWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "local", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	cl.signer = testSigner(func(raw []byte) ([]byte, error) {
		return append([]byte("DKIM-Signature: test\r\n"), raw...), nil
	})
	msg := &model.Message{ID: "m3", MailFrom: "sender@example.com", Data: []byte("From: sender@example.com\r\n\r\nhello")}
	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver local spool with signer: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if !strings.HasPrefix(string(b), "DKIM-Signature: test\r\n") {
		t.Fatalf("missing dkim signature prefix: %q", string(b))
	}
}

func TestDeliverLocalSpoolReturnsSignerError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "local", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	cl.signer = testSigner(func(raw []byte) ([]byte, error) {
		return nil, errors.New("sign failed")
	})
	msg := &model.Message{ID: "m4", MailFrom: "sender@example.com", Data: []byte("From: sender@example.com\r\n\r\nhello")}
	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err == nil {
		t.Fatal("expected signer error")
	}
}

func TestDeliverLocalSpoolAppliesARCSignerWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "local", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	cl.arcSigner = testSigner(func(raw []byte) ([]byte, error) {
		return append([]byte("ARC-Seal: test\r\n"), raw...), nil
	})
	msg := &model.Message{ID: "m5", MailFrom: "sender@example.com", Data: []byte("From: sender@example.com\r\n\r\nhello")}
	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver local spool with arc signer: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if !strings.HasPrefix(string(b), "ARC-Seal: test\r\n") {
		t.Fatalf("missing arc seal prefix: %q", string(b))
	}
}

func TestDeliverLocalSpoolDefaultsToLocalBackend(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m6", MailFrom: "sender@example.com", Data: []byte("Subject: hi\r\n\r\nhello")}

	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver local spool with default backend: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected spool file")
	}
}

func TestDeliverLocalSpoolRejectsUnknownBackend(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "memory", LocalSpoolDir: dir}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m7", MailFrom: "sender@example.com", Data: []byte("Subject: hi\r\n\r\nhello")}

	err := cl.Deliver(context.Background(), msg, "user@example.net")
	if err == nil {
		t.Fatal("expected spool backend error")
	}
	if !strings.Contains(err.Error(), "unknown spool backend") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeliverS3SpoolStoresObject(t *testing.T) {
	orig := newS3PutObjectClient
	t.Cleanup(func() { newS3PutObjectClient = orig })

	fake := &fakeS3Client{}
	newS3PutObjectClient = func(cfg config.Config) (s3PutObjectAPI, error) {
		return fake, nil
	}

	cfg := config.Config{
		DeliveryMode:          "local_spool",
		SpoolBackend:          "s3",
		SpoolS3Bucket:         "mail-spool",
		SpoolS3Prefix:         "archive",
		SpoolS3Region:         "ap-northeast-1",
		SpoolS3ForcePathStyle: true,
	}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m8", MailFrom: "sender@example.com", Data: []byte("From: sender@example.com\r\n\r\nhello")}

	if err := cl.Deliver(context.Background(), msg, "user@example.net"); err != nil {
		t.Fatalf("deliver s3 spool: %v", err)
	}
	if fake.bucket != "mail-spool" {
		t.Fatalf("bucket=%q", fake.bucket)
	}
	if fake.key != "archive/m8_user_example.net.eml" {
		t.Fatalf("key=%q", fake.key)
	}
	if fake.contentType != "message/rfc822" {
		t.Fatalf("contentType=%q", fake.contentType)
	}
	if !strings.Contains(string(fake.body), "hello") {
		t.Fatalf("unexpected body: %q", string(fake.body))
	}
	if fake.metadata["message-id"] != "m8" {
		t.Fatalf("metadata=%v", fake.metadata)
	}
}

func TestDeliverS3SpoolRequiresBucket(t *testing.T) {
	cfg := config.Config{DeliveryMode: "local_spool", SpoolBackend: "s3"}
	cl := NewClient(cfg)
	msg := &model.Message{ID: "m9", MailFrom: "sender@example.com", Data: []byte("Subject: hi\r\n\r\nhello")}

	err := cl.Deliver(context.Background(), msg, "user@example.net")
	if err == nil {
		t.Fatal("expected spool backend error")
	}
	if !strings.Contains(err.Error(), "MTA_SPOOL_S3_BUCKET") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type testSigner func([]byte) ([]byte, error)

func (s testSigner) Sign(raw []byte) ([]byte, error) {
	return s(raw)
}

type fakeS3Client struct {
	bucket      string
	key         string
	contentType string
	body        []byte
	metadata    map[string]string
}

func (f *fakeS3Client) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.bucket = *in.Bucket
	f.key = *in.Key
	if in.ContentType != nil {
		f.contentType = *in.ContentType
	}
	f.metadata = in.Metadata
	if in.Body != nil {
		body, err := io.ReadAll(in.Body)
		if err != nil {
			return nil, err
		}
		f.body = bytes.Clone(body)
	}
	return &s3.PutObjectOutput{}, nil
}
