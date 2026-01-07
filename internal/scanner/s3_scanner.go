package scanner

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ca-srg/ragent/internal/types"
)

// S3Scanner implements file scanning from S3 buckets
type S3Scanner struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Scanner creates a new S3Scanner instance
func NewS3Scanner(bucket, prefix, region string) (*S3Scanner, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(cfg)

	// Normalize prefix (ensure trailing slash for non-empty prefix)
	normalizedPrefix := prefix
	if normalizedPrefix != "" && !strings.HasSuffix(normalizedPrefix, "/") {
		normalizedPrefix += "/"
	}

	return &S3Scanner{
		client: client,
		bucket: bucket,
		prefix: normalizedPrefix,
	}, nil
}

// ScanBucket scans S3 bucket for supported files (markdown and CSV) with pagination
func (s *S3Scanner) ScanBucket(ctx context.Context) ([]*types.FileInfo, error) {
	var files []*types.FileInfo

	log.Printf("Scanning S3 bucket: s3://%s/%s", s.bucket, s.prefix)

	// Use paginator for handling large buckets
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})

	pageCount := 0
	for paginator.HasMorePages() {
		pageCount++
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects in S3 bucket: %w", err)
		}

		log.Printf("Processing page %d with %d objects", pageCount, len(page.Contents))

		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}

			key := *obj.Key

			// Skip directories (keys ending with /)
			if strings.HasSuffix(key, "/") {
				continue
			}

			// Check if it's a supported file type
			if !s.IsSupportedFile(key) {
				continue
			}

			// Get file size
			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}

			// Get last modified time
			var modTime time.Time
			if obj.LastModified != nil {
				modTime = *obj.LastModified
			}

			// Create FileInfo
			fileInfo := &types.FileInfo{
				Path:       fmt.Sprintf("s3://%s/%s", s.bucket, key),
				Name:       filepath.Base(key),
				Size:       size,
				ModTime:    modTime,
				IsMarkdown: s.IsMarkdownFile(key),
				IsCSV:      s.IsCSVFile(key),
			}

			files = append(files, fileInfo)
		}
	}

	log.Printf("Found %d supported files in S3 bucket", len(files))
	return files, nil
}

// DownloadFile downloads a file from S3 and returns its content
func (s *S3Scanner) DownloadFile(ctx context.Context, s3Path string) (string, error) {
	// Parse S3 path (s3://bucket/key)
	key, err := s.parseS3Path(s3Path)
	if err != nil {
		return "", err
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("failed to download file from S3: %w", err)
	}
	defer func() {
		if closeErr := result.Body.Close(); closeErr != nil {
			log.Printf("Warning: failed to close S3 object body: %v", closeErr)
		}
	}()

	content, err := io.ReadAll(result.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read S3 object content: %w", err)
	}

	return string(content), nil
}

// parseS3Path extracts the key from an S3 path (s3://bucket/key)
func (s *S3Scanner) parseS3Path(s3Path string) (string, error) {
	if !strings.HasPrefix(s3Path, "s3://") {
		return "", fmt.Errorf("invalid S3 path format: %s", s3Path)
	}

	// Remove s3:// prefix
	pathWithoutScheme := strings.TrimPrefix(s3Path, "s3://")

	// Split bucket and key
	parts := strings.SplitN(pathWithoutScheme, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid S3 path format: %s", s3Path)
	}

	return parts[1], nil
}

// ReadFileContent reads content from S3 file
// This method is compatible with FileScanner interface pattern
func (s *S3Scanner) ReadFileContent(filePath string) (string, error) {
	return s.DownloadFile(context.Background(), filePath)
}

// IsMarkdownFile checks if a file is a markdown file
func (s *S3Scanner) IsMarkdownFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".md" || ext == ".markdown"
}

// IsCSVFile checks if a file is a CSV file
func (s *S3Scanner) IsCSVFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".csv"
}

// IsSupportedFile checks if a file is a supported file type (markdown or CSV)
func (s *S3Scanner) IsSupportedFile(filePath string) bool {
	return s.IsMarkdownFile(filePath) || s.IsCSVFile(filePath)
}

// ValidateBucket checks if the S3 bucket is accessible
func (s *S3Scanner) ValidateBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("cannot access S3 bucket %s: %w", s.bucket, err)
	}
	return nil
}

// GetBucket returns the bucket name
func (s *S3Scanner) GetBucket() string {
	return s.bucket
}

// GetPrefix returns the prefix
func (s *S3Scanner) GetPrefix() string {
	return s.prefix
}

// IsS3Path checks if a path is an S3 path
func IsS3Path(path string) bool {
	return strings.HasPrefix(path, "s3://")
}
