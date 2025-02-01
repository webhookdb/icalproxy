package feedstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/fp"
	"github.com/webhookdb/icalproxy/internal"
	"io"
)

var ErrNotFound = errors.New("not found")

type Interface interface {
	// Store stores the feed bytes in storage.
	Store(ctx context.Context, feedId int64, body []byte) error
	// Fetch fetches the feed from storage and returns the bytes.
	// If the feed is not stored, return ErrNotFound as the error.
	Fetch(ctx context.Context, feedId int64) ([]byte, error)
}

func New(ctx context.Context, cfg config.Config) (*Storage, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion("auto"),
	}
	if cfg.S3AccessKeySecret != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyId, cfg.S3AccessKeySecret, "")))
	}
	awscfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, internal.ErrWrap(err, "creating aws config")
	}
	client := s3.NewFromConfig(awscfg, func(o *s3.Options) {
		o.UsePathStyle = true // Needed for localstack
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			// If we are using an explicit URL, assume it may not implement all the S3 security policies,
			// like https://github.com/aws/aws-sdk-js-v3/issues/6810 talks about.
			// In particular, R2 does not support checksums:
			// https://community.centminmod.com/threads/aws-s3-sdk-compatibility-inconsistencies-with-r2.27255/
			// We can make this configurable in the future if needed.
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
	})
	return &Storage{s3Client: client, bucket: &cfg.S3Bucket, prefix: cfg.S3Prefix}, nil

}

type Storage struct {
	s3Client *s3.Client
	bucket   *string
	prefix   string
}

func (s *Storage) S3Client() *s3.Client {
	return s.s3Client
}

func (s *Storage) Store(ctx context.Context, feedId int64, body []byte) error {
	if _, err := s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: s.bucket,
		Key:    s.key(feedId),
		Body:   bytes.NewReader(body),
	}); err != nil {
		return internal.ErrWrap(err, "s3 PutObject")
	}
	return nil
}

func (s *Storage) Fetch(ctx context.Context, feedId int64) ([]byte, error) {
	cacheObj, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: s.bucket,
		Key:    s.key(feedId),
	})
	if _, ok := fp.ErrorAs[*s3types.NoSuchKey](err); ok {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, internal.ErrWrap(err, "s3 GetObject")
	}
	return io.ReadAll(cacheObj.Body)
}

func (s *Storage) key(f int64) *string {
	return aws.String(fmt.Sprintf("%s/%d.ics", s.prefix, f))
}
