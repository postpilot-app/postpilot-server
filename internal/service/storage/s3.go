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
	client *s3.Client
	bucket string
	cdnURL string
	region string
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

	return &S3Service{
		client: s3.NewFromConfig(awsCfg),
		bucket: cfg.Bucket,
		cdnURL: cfg.CDNURL,
		region: cfg.Region,
	}, nil
}

// UploadFile uploads a multipart file to S3 and returns the public URL
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

	if s.cdnURL != "" {
		return fmt.Sprintf("%s/%s", strings.TrimRight(s.cdnURL, "/"), key), nil
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, key), nil
}
