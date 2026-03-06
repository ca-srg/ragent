package pdf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/ingestion/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestReaderConfig() PDFReaderConfig {
	return PDFReaderConfig{
		Provider: "bedrock",
		Model:    "test-model",
		Timeout:  30 * time.Second,
	}
}

func TestReader_ReadFileFromBytes_Success(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "Page 1 content", Title: "Test Doc", Category: "test", Tags: []string{"tag1"}, Summary: "Summary of page 1"},
				{PageIndex: 2, Text: "Page 2 content"},
				{PageIndex: 3, Text: "Page 3 content"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	pdfData := []byte("fake pdf bytes")
	files, err := reader.ReadFileFromBytes(pdfData, "/path/to/test.pdf")

	require.NoError(t, err)
	assert.Len(t, files, 3)

	// Check first page
	assert.Equal(t, "pdf:///path/to/test.pdf/page/1", files[0].Path)
	assert.True(t, files[0].IsPDF)
	assert.Equal(t, 1, files[0].PDFPageIndex)
	assert.Equal(t, "Page 1 content", files[0].Content)
	assert.Equal(t, "Test Doc", files[0].Metadata.Title)
	assert.Equal(t, "test", files[0].Metadata.Category)
	assert.Equal(t, []string{"tag1"}, files[0].Metadata.Tags)
	assert.Equal(t, "Summary of page 1", files[0].Metadata.CustomFields["summary"])

	// Check second page
	assert.Equal(t, "pdf:///path/to/test.pdf/page/2", files[1].Path)
	assert.Equal(t, 2, files[1].PDFPageIndex)
	assert.Equal(t, "Page 2 content", files[1].Content)

	// Check third page
	assert.Equal(t, 3, files[2].PDFPageIndex)
}

func TestReader_ReadFileFromBytes_EmptyPDFData(t *testing.T) {
	mockClient := &MockOCRClient{}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte{}, "/path/to/empty.pdf")
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestReader_ReadFileFromBytes_NilPDFData(t *testing.T) {
	mockClient := &MockOCRClient{}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes(nil, "/path/to/nil.pdf")
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestReader_ReadFileFromBytes_EmptyOCRResult(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/path/to/empty.pdf")
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestReader_ReadFileFromBytes_OCRError(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return nil, errors.New("OCR API error")
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	_, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/path/to/error.pdf")
	assert.Error(t, err)

	// ReadFileFromBytes wraps OCR errors in domain.ProcessingError
	var procErr *domain.ProcessingError
	assert.True(t, errors.As(err, &procErr), "error should be a *domain.ProcessingError")
	assert.True(t, procErr.Retryable, "OCR errors should be retryable")
	assert.Contains(t, procErr.Message, "OCR failed")
}

func TestReader_PageExpansion_PathFormat(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "content"},
				{PageIndex: 2, Text: "content"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 2)

	// Verify pdf:// path format: pdf://<filePath>/page/<N>
	assert.Equal(t, "pdf:///docs/report.pdf/page/1", files[0].Path)
	assert.Equal(t, "pdf:///docs/report.pdf/page/2", files[1].Path)
}

func TestReader_PageExpansion_MetadataMapping(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{
					PageIndex: 1,
					Text:      "Hello world",
					Title:     "My Title",
					Category:  "engineering",
					Tags:      []string{"go", "pdf"},
					Summary:   "A summary",
				},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "My Title", f.Metadata.Title)
	assert.Equal(t, "engineering", f.Metadata.Category)
	assert.Equal(t, []string{"go", "pdf"}, f.Metadata.Tags)
	assert.True(t, f.IsPDF)
	assert.Equal(t, 1, f.PDFPageIndex)
	assert.Equal(t, "Hello world", f.Content)
	// Word count: "Hello world" = 2 words
	assert.Equal(t, 2, f.Metadata.WordCount)
	// Summary is stored in CustomFields, not a top-level field
	assert.Equal(t, "A summary", f.Metadata.CustomFields["summary"])
	// Source should be the base filename
	assert.Equal(t, "report.pdf", f.Metadata.Source)
	// FilePath and Reference should be the original path
	assert.Equal(t, "/docs/report.pdf", f.Metadata.FilePath)
	assert.Equal(t, "/docs/report.pdf", f.Metadata.Reference)
}

func TestReader_PageExpansion_TitleFallback(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "content", Title: ""},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/my-report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 1)

	// When Title is empty, it should fall back to filename without extension
	assert.Equal(t, "my-report", files[0].Metadata.Title)
}

func TestReader_PageExpansion_CategoryFallback(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "content", Category: ""},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/engineering/report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 1)

	// When Category is empty, it should fall back to directory name
	assert.Equal(t, "engineering", files[0].Metadata.Category)
}

func TestReader_PageExpansion_SkipsEmptyTextPages(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "valid content"},
				{PageIndex: 2, Text: ""},
				{PageIndex: 3, Text: "   "},
				{PageIndex: 4, Text: "also valid"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	// Pages with empty or whitespace-only text should be skipped by pageToFileInfo
	assert.Len(t, files, 2)
	assert.Equal(t, 1, files[0].PDFPageIndex)
	assert.Equal(t, 4, files[1].PDFPageIndex)
}

func TestReader_PageExpansion_NilPageSkipped(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "valid"},
				nil,
				{PageIndex: 3, Text: "also valid"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	// nil page should be skipped by pageToFileInfo
	assert.Len(t, files, 2)
}

func TestReader_PageExpansion_NameFormat(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 5, Text: "content"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 1)

	// Name format: "<filename> page <N>"
	assert.Equal(t, "report.pdf page 5", files[0].Name)
}

func TestReader_ReadFileFromBytes_PassesFilenameToClient(t *testing.T) {
	var receivedFilename string
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			receivedFilename = filename
			return []*PageResult{}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	_, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/some/deep/path/document.pdf")
	require.NoError(t, err)

	// Reader should pass only the base filename to the OCR client
	assert.Equal(t, "document.pdf", receivedFilename)
}

func TestReader_ReadFileFromBytes_SizeIsTextLength(t *testing.T) {
	mockClient := &MockOCRClient{
		ExtractPagesFunc: func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
			return []*PageResult{
				{PageIndex: 1, Text: "twelve chars"},
			}, nil
		},
	}
	reader := NewReader(mockClient, newTestReaderConfig())

	files, err := reader.ReadFileFromBytes([]byte("fake pdf"), "/docs/report.pdf")
	require.NoError(t, err)
	require.Len(t, files, 1)

	// Size should be the length of the text content
	assert.Equal(t, int64(len("twelve chars")), files[0].Size)
}
