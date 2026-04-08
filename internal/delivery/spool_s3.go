package delivery

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
)

type s3PutObjectAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type s3SpoolStore struct {
	cfg       config.Config
	client    s3PutObjectAPI
	prepareFn func([]byte) ([]byte, error)
}

var newS3PutObjectClient = func(cfg config.Config) (s3PutObjectAPI, error) {
	loadOpts := make([]func(*awsconfig.LoadOptions) error, 0, 3)
	region := strings.TrimSpace(cfg.SpoolS3Region)
	if region == "" {
		region = "us-east-1"
	}
	loadOpts = append(loadOpts, awsconfig.WithRegion(region))

	accessKey := strings.TrimSpace(cfg.SpoolS3AccessKey)
	secretKey := strings.TrimSpace(cfg.SpoolS3SecretKey)
	if accessKey != "" || secretKey != "" {
		if accessKey == "" || secretKey == "" {
			return nil, fmt.Errorf("spool s3 credentials require both access key and secret key")
		}
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, err
	}

	options := func(o *s3.Options) {
		o.UsePathStyle = cfg.SpoolS3ForcePathStyle
		if endpoint := strings.TrimSpace(cfg.SpoolS3Endpoint); endpoint != "" {
			if !strings.Contains(endpoint, "://") {
				scheme := "https://"
				if !cfg.SpoolS3UseTLS {
					scheme = "http://"
				}
				endpoint = scheme + endpoint
			}
			o.BaseEndpoint = &endpoint
		}
	}

	return s3.NewFromConfig(awsCfg, options), nil
}

func newS3SpoolStore(cfg config.Config, prepareFn func([]byte) ([]byte, error)) (spoolBackend, error) {
	if strings.TrimSpace(cfg.SpoolS3Bucket) == "" {
		return nil, fmt.Errorf("spool s3 backend requires MTA_SPOOL_S3_BUCKET")
	}
	client, err := newS3PutObjectClient(cfg)
	if err != nil {
		return nil, err
	}
	return &s3SpoolStore{cfg: cfg, client: client, prepareFn: prepareFn}, nil
}

func (s *s3SpoolStore) Store(msg *model.Message, rcpt string) error {
	key := spoolObjectKey(s.cfg, msg, rcpt)
	payload, err := s.prepareFn(msg.Data)
	if err != nil {
		return err
	}

	_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &s.cfg.SpoolS3Bucket,
		Key:         &key,
		Body:        bytes.NewReader(payload),
		ContentType: strPtr("message/rfc822"),
		Metadata: map[string]string{
			"message-id": strings.TrimSpace(msg.ID),
			"rcpt-to":    rcpt,
		},
		StorageClass: types.StorageClassStandard,
	})
	return err
}

func spoolObjectKey(cfg config.Config, msg *model.Message, rcpt string) string {
	prefix := strings.Trim(strings.TrimSpace(cfg.SpoolS3Prefix), "/")
	id := strings.TrimSpace(msg.ID)
	if id == "" {
		id = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	name := fmt.Sprintf("%s_%s.eml", id, sanitizeFilename(rcpt))
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func strPtr(v string) *string {
	return &v
}
