package storage

import (
	"context"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/postpilot-dev/postpilot-server/internal/config"
	"github.com/rs/xid"
)

type S3Service struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
	cdnURL        string
	region        string
}

func NewS3Service(cfg config.S3Config) (*S3Service, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey, cfg.SecretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)

	return &S3Service{
		client:        client,
		presignClient: s3.NewPresignClient(client),
		bucket:        cfg.Bucket,
		cdnURL:        cfg.CDNURL,
		region:        cfg.Region,
	}, nil
}

// UploadFile uploads a multipart file to S3 and returns a presigned URL (1 hour expiry)
func (s *S3Service) UploadFile(ctx context.Context, file *multipart.FileHeader) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer src.Close()

	ext := strings.ToLower(filepath.Ext(file.Filename))
	key := fmt.Sprintf("uploads/%s/%s%s",
		time.Now().Format("2006/01/02"),
		xid.New().String(),
		ext,
	)

	contentType := "image/jpeg"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        src,
		ContentType: &contentType,
	})
	if err != nil {
		return "", fmt.Errorf("s3 put object: %w", err)
	}

	presignedURL, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(1*time.Hour))
	if err != nil {
		return "", fmt.Errorf("presign get object: %w", err)
	}

	return presignedURL.URL, nil
}

// UploadFileReturnKey uploads a file and returns both the presigned URL and the S3 key
func (s *S3Service) UploadFileReturnKey(ctx context.Context, file *multipart.FileHeader) (url, key string, err error) {
	src, err := file.Open()
	if err != nil {
		return "", "", fmt.Errorf("open file: %w", err)
	}
	defer src.Close()

	ext := strings.ToLower(filepath.Ext(file.Filename))
	key = fmt.Sprintf("uploads/%s/%s%s",
		time.Now().Format("2006/01/02"),
		xid.New().String(),
		ext,
	)

	contentType := "image/jpeg"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        src,
		ContentType: &contentType,
	})
	if err != nil {
		return "", "", fmt.Errorf("s3 put object: %w", err)
	}

	presignedURL, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(1*time.Hour))
	if err != nil {
		return "", "", fmt.Errorf("presign get object: %w", err)
	}

	return presignedURL.URL, key, nil
}

// DeleteFile deletes a single file from S3 by key
func (s *S3Service) DeleteFile(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	return err
}

// DeleteFiles deletes multiple files from S3
func (s *S3Service) DeleteFiles(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := s.DeleteFile(ctx, key); err != nil {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return nil
}
