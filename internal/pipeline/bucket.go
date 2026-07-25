package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// ensureBucket creates the target bucket when using S3-compatible storage.
func ensureBucket(ctx context.Context, cfg StoreConfig) error {
	switch cfg.Backend {
	case BackendMemory:
		return nil
	case BackendMinIO:
		return ensureS3Bucket(ctx, minioBucketURL(cfg.MinBucket, cfg.MinEndpoint), BackendMinIO)
	case BackendTigris:
		return ensureS3Bucket(ctx, tigrisBucketURL(cfg.TigrisBucket), BackendTigris)
	default:
		return fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}

func ensureS3Bucket(ctx context.Context, bucketURL string, backend Backend) error {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return fmt.Errorf("parse bucket url: %w", err)
	}
	if u.Host == "" {
		return fmt.Errorf("bucket name missing in url %q", bucketURL)
	}

	ensureS3Env(StoreConfig{Backend: backend})

	endpoint := u.Query().Get("endpoint")
	region := u.Query().Get("region")
	if region == "" {
		region = envOr("AWS_REGION", "us-east-1")
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			os.Getenv("AWS_ACCESS_KEY_ID"),
			os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"",
		)),
	)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = true
	})

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(u.Host),
	})
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "BucketAlreadyOwnedByYou" || apiErr.ErrorCode() == "BucketAlreadyExists" {
			return nil
		}
	}

	return fmt.Errorf("create bucket %q: %w", u.Host, err)
}

func isBucketMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchBucket") ||
		strings.Contains(msg, "NotFound") && strings.Contains(msg, "ListObjects")
}
