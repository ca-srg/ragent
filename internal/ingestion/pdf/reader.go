package pdf

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/ingestion/domain"
	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
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
func (r *Reader) ReadFile(filePath string) ([]*domain.FileInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF file %s: %w", filePath, err)
	}
	return r.ReadFileFromBytes(data, filePath)
}

// ReadFileFromBytes reads a PDF from bytes and returns one FileInfo per page.
// Used for S3 and GitHub sources where content is already in memory.
func (r *Reader) ReadFileFromBytes(pdfData []byte, filePath string) ([]*domain.FileInfo, error) {
	if len(pdfData) == 0 {
		log.Printf("Warning: empty PDF data for %s", filePath)
		return []*domain.FileInfo{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.config.Timeout)
	defer cancel()

	filename := filepath.Base(filePath)

	log.Printf("Running OCR on PDF: %s (%d bytes)", filePath, len(pdfData))
	pages, err := r.client.ExtractPages(ctx, pdfData, filename)
	if err != nil {
		return nil, &domain.ProcessingError{
			Type:      pkgconfig.ErrorTypeOCR,
			Message:   fmt.Sprintf("OCR failed for %s: %v", filePath, err),
			FilePath:  filePath,
			Timestamp: time.Now(),
			Retryable: true,
		}
	}

	if len(pages) == 0 {
		log.Printf("Warning: OCR returned 0 pages for %s", filePath)
		return []*domain.FileInfo{}, nil
	}

	var files []*domain.FileInfo
	for _, page := range pages {
		fileInfo := r.pageToFileInfo(page, filePath)
		if fileInfo != nil {
			files = append(files, fileInfo)
		}
	}

	log.Printf("PDF %s expanded to %d page FileInfo entries", filePath, len(files))
	return files, nil
}

// pageToFileInfo converts a PageResult to a domain.FileInfo.
func (r *Reader) pageToFileInfo(page *PageResult, filePath string) *domain.FileInfo {
	if page == nil || strings.TrimSpace(page.Text) == "" {
		return nil
	}

	path := fmt.Sprintf("pdf://%s/page/%d", filePath, page.PageIndex)
	filename := filepath.Base(filePath)
	dirName := filepath.Dir(filePath)

	// Title: use OCR result, fallback to filename
	title := page.Title
	if title == "" {
		title = strings.TrimSuffix(filename, filepath.Ext(filename))
	}

	// Category: use OCR result, fallback to directory name
	category := page.Category
	if category == "" {
		category = filepath.Base(dirName)
	}

	return &domain.FileInfo{
		Path:         path,
		Name:         fmt.Sprintf("%s page %d", filename, page.PageIndex),
		Size:         int64(len(page.Text)),
		ModTime:      time.Now(),
		IsPDF:        true,
		PDFPageIndex: page.PageIndex,
		Content:      page.Text,
		Metadata: domain.DocumentMetadata{
			Title:     title,
			Category:  category,
			Tags:      page.Tags,
			Source:    filename,
			FilePath:  filePath,
			Reference: filePath,
			WordCount: len(strings.Fields(page.Text)),
			CustomFields: map[string]interface{}{
				"summary": page.Summary,
			},
		},
	}
}
