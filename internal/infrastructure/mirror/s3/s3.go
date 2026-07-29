// Package s3 is the mirror.Store for any S3-compatible bucket, on
// aws-sdk-go-v2. Without keys in config.json the SDK's credential chain
// applies: environment, shared config, IAM role, IRSA, SSO.
package s3

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"

	"github.com/zenkiet/edge-gateway/internal/domain"
	"github.com/zenkiet/edge-gateway/internal/infrastructure/mirror"
)

type Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func New(src domain.Source) (*Store, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(src.Region),
		config.WithLogger(logging.Nop{}),
		config.WithHTTPClient(&http.Client{Timeout: 5 * time.Minute}),
	}
	if src.AccessKeyID != "" {
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(src.AccessKeyID, src.SecretAccessKey, "")))
	}
	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if src.Endpoint != "" {
			o.BaseEndpoint = aws.String(src.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &Store{client: client, bucket: src.Bucket, prefix: src.Prefix}, nil
}

func (s *Store) List(ctx context.Context) ([]mirror.Object, error) {
	var out []mirror.Object
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &s.bucket, Prefix: &s.prefix})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, mirror.Object{Key: strings.TrimPrefix(aws.ToString(o.Key), s.prefix), Size: aws.ToInt64(o.Size), ModTime: aws.ToTime(o.LastModified)})
		}
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: aws.String(s.prefix + key)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
