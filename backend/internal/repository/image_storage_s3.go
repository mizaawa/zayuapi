package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// S3ImageStorage 用 S3 兼容对象存储实现 service.ImageStorage。
type S3ImageStorage struct {
	client        *s3.Client
	bucket        string
	publicBaseURL string
	presignExpiry time.Duration
}

var _ service.ImageStorage = (*S3ImageStorage)(nil)
var _ service.ImageStorageCleaner = (*S3ImageStorage)(nil)
var _ service.ImageStorageReader = (*S3ImageStorage)(nil)

// NewS3ImageStorage 依据配置构造 S3 图片存储（调用方应先确认 cfg.Active()）。
func NewS3ImageStorage(ctx context.Context, cfg *config.ImageStorageConfig) (*S3ImageStorage, error) {
	client, err := newS3Client(ctx, s3ClientParams{
		Endpoint:        cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		ForcePathStyle:  cfg.ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}

	expiry := time.Duration(cfg.PresignExpiry) * time.Hour
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}

	return &S3ImageStorage{
		client:        client,
		bucket:        cfg.Bucket,
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
		presignExpiry: expiry,
	}, nil
}

// Save 上传图片字节，返回可访问 URL：配了 public_base_url 则返回公开直链，否则返回 presigned 临时链接。
func (s *S3ImageStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	finish := servertiming.ObserveDependency(ctx, "s3")
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	finish()
	if err != nil {
		return "", fmt.Errorf("S3 PutObject: %w", err)
	}

	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + strings.TrimLeft(key, "/"), nil
	}

	presignClient := s3.NewPresignClient(s.client)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(s.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return result.URL, nil
}

// Read uses the administrator-configured endpoint, including private MinIO,
// without exposing bucket URLs or signing credentials to the browser.
func (s *S3ImageStorage) Read(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	if maxBytes <= 0 {
		return nil, "", fmt.Errorf("invalid image read limit")
	}
	finish := servertiming.ObserveDependency(ctx, "s3")
	defer finish()
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, "", fmt.Errorf("S3 GetObject: %w", err)
	}
	defer func() { _ = object.Body.Close() }()
	if aws.ToInt64(object.ContentLength) > maxBytes {
		return nil, "", fmt.Errorf("stored image exceeds read limit")
	}
	data, err := io.ReadAll(io.LimitReader(object.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read stored image: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("stored image exceeds read limit")
	}
	return data, aws.ToString(object.ContentType), nil
}

// DeletePrefix removes every object below prefix in bounded batches. The
// caller supplies the already-isolated image prefix; no user-controlled value
// is ever used as the bucket or a parent path.
func (s *S3ImageStorage) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	prefix = strings.TrimSpace(prefix)
	if s == nil || s.client == nil || prefix == "" {
		return 0, nil
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &prefix,
	})
	var deleted int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return deleted, fmt.Errorf("S3 ListObjectsV2: %w", err)
		}
		identifiers := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			if object.Key == nil || strings.TrimSpace(*object.Key) == "" {
				continue
			}
			identifiers = append(identifiers, types.ObjectIdentifier{Key: object.Key})
		}
		for start := 0; start < len(identifiers); start += 1000 {
			end := start + 1000
			if end > len(identifiers) {
				end = len(identifiers)
			}
			output, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: &s.bucket,
				Delete: &types.Delete{Objects: identifiers[start:end], Quiet: aws.Bool(false)},
			})
			if err != nil {
				return deleted, fmt.Errorf("S3 DeleteObjects: %w", err)
			}
			if output != nil {
				deleted += int64(len(output.Deleted))
				if len(output.Errors) > 0 {
					return deleted, fmt.Errorf("S3 DeleteObjects: %d object deletions failed", len(output.Errors))
				}
			}
		}
	}
	return deleted, nil
}
