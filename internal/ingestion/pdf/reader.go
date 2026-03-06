package pdf

import (
	"github.com/ca-srg/ragent/internal/ingestion/domain"
)

// Reader converts PDF files into domain.FileInfo slices (one per page).
type Reader struct {
	client OCRClient
	config PDFReaderConfig
}

// NewReader creates a new PDF Reader with the given OCR client and configuration.
func NewReader(client OCRClient, config PDFReaderConfig) *Reader {
	return &Reader{
		client: client,
		config: config,
	}
}

// ReadFile reads a local PDF file and returns one FileInfo per page.
// This method is a stub - full implementation is in Task 7.
func (r *Reader) ReadFile(filePath string) ([]*domain.FileInfo, error) {
	return r.ReadFileFromBytes(nil, filePath)
}

// ReadFileFromBytes reads a PDF from bytes and returns one FileInfo per page.
// Used for S3 and GitHub sources. This method is a stub - full implementation is in Task 7.
func (r *Reader) ReadFileFromBytes(pdfData []byte, filePath string) ([]*domain.FileInfo, error) {
	// TODO: implement in Task 7
	return nil, nil
}
