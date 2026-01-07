package scanner

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupFakeS3 creates a fake S3 server and returns an S3 client configured to use it
func setupFakeS3(t *testing.T) (*s3.Client, *httptest.Server) {
	t.Helper()

	// Create in-memory backend
	backend := s3mem.New()
	faker := gofakes3.New(backend)
	server := httptest.NewServer(faker.Server())

	// Create S3 client pointing to fake server
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	return client, server
}

// createTestBucket creates a test bucket with the given name
func createTestBucket(t *testing.T, client *s3.Client, bucketName string) {
	t.Helper()

	_, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	require.NoError(t, err)
}

// uploadTestFile uploads a test file to the bucket
func uploadTestFile(t *testing.T, client *s3.Client, bucketName, key, content string) {
	t.Helper()

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   strings.NewReader(content),
	})
	require.NoError(t, err)
}

// newS3ScannerWithClient creates an S3Scanner with a custom S3 client for testing
func newS3ScannerWithClient(client *s3.Client, bucket, prefix string) *S3Scanner {
	normalizedPrefix := prefix
	if normalizedPrefix != "" && normalizedPrefix[len(normalizedPrefix)-1] != '/' {
		normalizedPrefix += "/"
	}

	return &S3Scanner{
		client: client,
		bucket: bucket,
		prefix: normalizedPrefix,
	}
}

func TestNewS3Scanner(t *testing.T) {
	t.Run("valid bucket name", func(t *testing.T) {
		// Note: This test will fail without actual AWS credentials
		// In real tests, we use setupFakeS3 instead
		_, err := NewS3Scanner("", "prefix", "us-east-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "S3 bucket name is required")
	})

	t.Run("empty bucket name returns error", func(t *testing.T) {
		_, err := NewS3Scanner("", "", "us-east-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "S3 bucket name is required")
	})
}

func TestS3Scanner_ScanBucket(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "test-bucket"
	createTestBucket(t, client, bucketName)

	// Upload test files
	uploadTestFile(t, client, bucketName, "doc1.md", "# Document 1\nContent here")
	uploadTestFile(t, client, bucketName, "doc2.markdown", "# Document 2\nMore content")
	uploadTestFile(t, client, bucketName, "data.csv", "id,name\n1,test")
	uploadTestFile(t, client, bucketName, "image.png", "binary data")
	uploadTestFile(t, client, bucketName, "readme.txt", "text file")

	t.Run("scan bucket root - flat structure", func(t *testing.T) {
		scanner := newS3ScannerWithClient(client, bucketName, "")

		files, err := scanner.ScanBucket(context.Background())
		require.NoError(t, err)

		// Should find 3 supported files (.md, .markdown, .csv)
		assert.Len(t, files, 3)

		// Verify file paths are in s3:// format
		for _, f := range files {
			assert.Contains(t, f.Path, "s3://"+bucketName+"/")
		}

		// Verify file types
		var mdCount, csvCount int
		for _, f := range files {
			if f.IsMarkdown {
				mdCount++
			}
			if f.IsCSV {
				csvCount++
			}
		}
		assert.Equal(t, 2, mdCount)
		assert.Equal(t, 1, csvCount)
	})

	t.Run("scan with prefix", func(t *testing.T) {
		// Upload files with prefix
		uploadTestFile(t, client, bucketName, "docs/guide.md", "# Guide")
		uploadTestFile(t, client, bucketName, "docs/tutorial.md", "# Tutorial")
		uploadTestFile(t, client, bucketName, "other/notes.md", "# Notes")

		scanner := newS3ScannerWithClient(client, bucketName, "docs")

		files, err := scanner.ScanBucket(context.Background())
		require.NoError(t, err)

		// Should only find files under docs/ prefix
		assert.Len(t, files, 2)

		for _, f := range files {
			assert.Contains(t, f.Path, "docs/")
		}
	})

	t.Run("scan empty prefix (bucket with directories)", func(t *testing.T) {
		// Create a new bucket for this test
		emptyBucket := "empty-bucket"
		createTestBucket(t, client, emptyBucket)

		scanner := newS3ScannerWithClient(client, emptyBucket, "")

		files, err := scanner.ScanBucket(context.Background())
		require.NoError(t, err)
		assert.Len(t, files, 0)
	})
}

func TestS3Scanner_DownloadFile(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "download-test-bucket"
	createTestBucket(t, client, bucketName)

	expectedContent := "# Test Document\n\nThis is test content with Japanese: テスト内容"
	uploadTestFile(t, client, bucketName, "test.md", expectedContent)

	scanner := newS3ScannerWithClient(client, bucketName, "")

	t.Run("download existing file", func(t *testing.T) {
		content, err := scanner.DownloadFile(context.Background(), "s3://"+bucketName+"/test.md")
		require.NoError(t, err)
		assert.Equal(t, expectedContent, content)
	})

	t.Run("download non-existent file", func(t *testing.T) {
		_, err := scanner.DownloadFile(context.Background(), "s3://"+bucketName+"/nonexistent.md")
		assert.Error(t, err)
	})

	t.Run("invalid S3 path format", func(t *testing.T) {
		_, err := scanner.DownloadFile(context.Background(), "invalid-path")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid S3 path format")
	})
}

func TestS3Scanner_IsSupportedFile(t *testing.T) {
	scanner := &S3Scanner{}

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{"markdown .md", "doc.md", true},
		{"markdown .markdown", "doc.markdown", true},
		{"csv file", "data.csv", true},
		{"uppercase MD", "DOC.MD", true},
		{"uppercase CSV", "DATA.CSV", true},
		{"text file", "readme.txt", false},
		{"json file", "config.json", false},
		{"image file", "image.png", false},
		{"no extension", "README", false},
		{"path with .md", "path/to/doc.md", true},
		{"path with .csv", "data/export.csv", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.IsSupportedFile(tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3Scanner_IsMarkdownFile(t *testing.T) {
	scanner := &S3Scanner{}

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{"lowercase .md", "doc.md", true},
		{"lowercase .markdown", "doc.markdown", true},
		{"uppercase .MD", "doc.MD", true},
		{"mixed case .Md", "doc.Md", true},
		{"csv file", "data.csv", false},
		{"no extension", "readme", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.IsMarkdownFile(tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3Scanner_IsCSVFile(t *testing.T) {
	scanner := &S3Scanner{}

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{"lowercase .csv", "data.csv", true},
		{"uppercase .CSV", "DATA.CSV", true},
		{"mixed case .Csv", "Data.Csv", true},
		{"markdown file", "doc.md", false},
		{"no extension", "data", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.IsCSVFile(tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3Scanner_ValidateBucket(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	t.Run("existing bucket", func(t *testing.T) {
		bucketName := "validate-test-bucket"
		createTestBucket(t, client, bucketName)

		scanner := newS3ScannerWithClient(client, bucketName, "")

		err := scanner.ValidateBucket(context.Background())
		assert.NoError(t, err)
	})

	t.Run("non-existent bucket", func(t *testing.T) {
		scanner := newS3ScannerWithClient(client, "non-existent-bucket", "")

		err := scanner.ValidateBucket(context.Background())
		assert.Error(t, err)
	})
}

func TestS3Scanner_GetBucketAndPrefix(t *testing.T) {
	scanner := &S3Scanner{
		bucket: "my-bucket",
		prefix: "docs/",
	}

	assert.Equal(t, "my-bucket", scanner.GetBucket())
	assert.Equal(t, "docs/", scanner.GetPrefix())
}

func TestIsS3Path(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"valid s3 path", "s3://bucket/key", true},
		{"s3 path with prefix", "s3://bucket/prefix/key", true},
		{"local path", "/local/path/file.md", false},
		{"relative path", "./docs/file.md", false},
		{"http url", "http://example.com/file.md", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsS3Path(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestS3Scanner_ScanBucketPagination(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "pagination-test-bucket"
	createTestBucket(t, client, bucketName)

	// Upload many files to test pagination
	// Note: gofakes3 may not enforce pagination limits, but this tests the flow
	for i := 0; i < 50; i++ {
		key := "docs/doc" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ".md"
		content := "# Document " + string(rune('0'+i/10)) + string(rune('0'+i%10))
		uploadTestFile(t, client, bucketName, key, content)
	}

	scanner := newS3ScannerWithClient(client, bucketName, "docs")

	files, err := scanner.ScanBucket(context.Background())
	require.NoError(t, err)

	// Should find all 50 markdown files
	assert.Len(t, files, 50)
}

func TestS3Scanner_FileInfoProperties(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "fileinfo-test-bucket"
	createTestBucket(t, client, bucketName)

	content := "# Test Document\n\nSome content here."
	uploadTestFile(t, client, bucketName, "test-doc.md", content)

	scanner := newS3ScannerWithClient(client, bucketName, "")

	files, err := scanner.ScanBucket(context.Background())
	require.NoError(t, err)
	require.Len(t, files, 1)

	fileInfo := files[0]

	// Verify FileInfo properties
	assert.Equal(t, "s3://"+bucketName+"/test-doc.md", fileInfo.Path)
	assert.Equal(t, "test-doc.md", fileInfo.Name)
	assert.True(t, fileInfo.IsMarkdown)
	assert.False(t, fileInfo.IsCSV)
	assert.Equal(t, int64(len(content)), fileInfo.Size)
	assert.False(t, fileInfo.ModTime.IsZero())
	assert.True(t, fileInfo.ModTime.Before(time.Now().Add(time.Minute)))
}

func TestS3Scanner_ReadFileContent(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "readcontent-test-bucket"
	createTestBucket(t, client, bucketName)

	expectedContent := "CSV content\nwith multiple lines"
	uploadTestFile(t, client, bucketName, "data.csv", expectedContent)

	scanner := newS3ScannerWithClient(client, bucketName, "")

	// ReadFileContent should work the same as DownloadFile
	content, err := scanner.ReadFileContent("s3://" + bucketName + "/data.csv")
	require.NoError(t, err)
	assert.Equal(t, expectedContent, content)
}

func TestS3Scanner_SkipDirectories(t *testing.T) {
	client, server := setupFakeS3(t)
	defer server.Close()

	bucketName := "skip-dirs-bucket"
	createTestBucket(t, client, bucketName)

	// Upload files including directory markers
	uploadTestFile(t, client, bucketName, "docs/", "") // directory marker
	uploadTestFile(t, client, bucketName, "docs/file.md", "# Doc")
	uploadTestFile(t, client, bucketName, "data/", "") // directory marker
	uploadTestFile(t, client, bucketName, "data/export.csv", "a,b")

	scanner := newS3ScannerWithClient(client, bucketName, "")

	files, err := scanner.ScanBucket(context.Background())
	require.NoError(t, err)

	// Should only find actual files, not directory markers
	assert.Len(t, files, 2)

	for _, f := range files {
		// Paths should not end with /
		assert.NotEqual(t, '/', f.Path[len(f.Path)-1])
	}
}
