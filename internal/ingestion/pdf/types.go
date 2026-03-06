package pdf

import (
	"context"
	"time"
)

// OCRClient defines the interface for OCR operations on PDF documents.
// Implementations can use different backends (Bedrock, etc.).
type OCRClient interface {
	// ExtractPages performs OCR on a PDF document and returns per-page results.
	ExtractPages(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error)

	// ValidateConnection checks if the OCR service is accessible.
	ValidateConnection(ctx context.Context) error
}

// PageResult represents the OCR result for a single PDF page.
type PageResult struct {
	PageIndex int      `json:"page_index"` // 1-based page number
	Text      string   `json:"text"`
	Title     string   `json:"title"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	Summary   string   `json:"summary"`
	Author    string   `json:"author"`
}

// PDFReaderConfig holds configuration for the PDF Reader.
type PDFReaderConfig struct {
	Provider    string        // OCR provider name (e.g., "bedrock")
	Model       string        // Model ID for OCR
	Timeout     time.Duration
	Concurrency int           // Number of concurrent OCR requests (for page-level parallelism)
}
