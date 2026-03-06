package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpumodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"google.golang.org/genai"
)

const (
	// defaultGeminiModel is the default Gemini model for OCR.
	defaultGeminiModel = "gemini-2.5-flash"
)

// GeminiOCRClient implements the OCRClient interface using Google Gemini API.
type GeminiOCRClient struct {
	genaiClient *genai.Client
	model       string
	timeout     time.Duration
	maxTokens   int32
	concurrency int
}

// NewGeminiOCRClient creates a new GeminiOCRClient.
func NewGeminiOCRClient(apiKey string, model string, timeout time.Duration, maxTokens int, concurrency int) (*GeminiOCRClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required for OCR_PROVIDER=gemini")
	}
	if model == "" {
		model = defaultGeminiModel
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if maxTokens <= 0 {
		maxTokens = 200000
	}
	if maxTokens <= 0 {
		maxTokens = 200000
	}
	if concurrency <= 0 {
		concurrency = 5
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &GeminiOCRClient{
		genaiClient: client,
		model:       model,
		timeout:     timeout,
		maxTokens:   int32(maxTokens),
		concurrency: concurrency,
	}, nil
}

// ExtractPages performs OCR on a PDF document and returns per-page results.
func (c *GeminiOCRClient) ExtractPages(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	if len(pdfData) == 0 {
		return nil, fmt.Errorf("PDF data is empty")
	}

	// Split large PDFs into individual pages before sending to Gemini.
	if len(pdfData) > maxPDFSizeBytes {
		log.Printf("PDF %s is large (%d bytes), splitting into pages", filename, len(pdfData))
		return c.extractPagesFromLargePDF(ctx, pdfData, filename)
	}

	// Split PDFs with many pages to avoid output token truncation.
	if pageCount := countPDFPages(pdfData); pageCount > maxPagesForSingleCall {
		log.Printf("PDF %s has %d pages (> %d), splitting into batches to avoid token truncation", filename, pageCount, maxPagesForSingleCall)
		return c.extractPagesFromLargePDF(ctx, pdfData, filename)
	}

	return c.callGemini(ctx, pdfData, filename)
}

// extractPagesFromLargePDF splits a large PDF into batches of pages and processes each batch.
// It uses pdfcpu to extract individual pages, merges them into batches, and sends each batch
// to the OCR API with retry logic for resilience against rate limiting.
func (c *GeminiOCRClient) extractPagesFromLargePDF(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	conf := pdfcpumodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpumodel.ValidationRelaxed

	pdfCtx, err := pdfcpuapi.ReadAndValidate(bytes.NewReader(pdfData), conf)
	if err != nil {
		log.Printf("Warning: failed to parse PDF %s: %v, falling back to direct processing", filename, err)
		return c.callGemini(ctx, pdfData, filename)
	}

	pageCount := pdfCtx.PageCount

	// Extract all page data first (pdfcpu is not concurrency-safe).
	type pageData struct {
		index int
		data  []byte
	}
	var pages []pageData
	for i := 1; i <= pageCount; i++ {
		pageReader, err := pdfcpuapi.ExtractPage(pdfCtx, i)
		if err != nil {
			log.Printf("Warning: failed to extract page %d of PDF %s: %v, skipping", i, filename, err)
			continue
		}
		pd, err := io.ReadAll(pageReader)
		if err != nil {
			log.Printf("Warning: failed to read page %d of PDF %s: %v, skipping", i, filename, err)
			continue
		}
		pages = append(pages, pageData{index: i, data: pd})
	}

	// Group pages into batches of maxPagesForSingleCall.
	var batches []batchInfo
	for i := 0; i < len(pages); i += maxPagesForSingleCall {
		end := i + maxPagesForSingleCall
		if end > len(pages) {
			end = len(pages)
		}
		batch := pages[i:end]

		var pagePDFs [][]byte
		var pageIndices []int
		for _, p := range batch {
			pagePDFs = append(pagePDFs, p.data)
			pageIndices = append(pageIndices, p.index)
		}

		merged, err := mergePagePDFs(pagePDFs)
		if err != nil {
			log.Printf("Warning: failed to merge batch (pages %d-%d) of PDF %s: %v, processing individually",
				pageIndices[0], pageIndices[len(pageIndices)-1], filename, err)
			// Fallback: process pages in this batch individually.
			for _, p := range batch {
				batches = append(batches, batchInfo{
					startPage: p.index,
					endPage:   p.index,
					data:      p.data,
					pages:     []int{p.index},
				})
			}
			continue
		}

		batches = append(batches, batchInfo{
			startPage: pageIndices[0],
			endPage:   pageIndices[len(pageIndices)-1],
			data:      merged,
			pages:     pageIndices,
		})
	}

	log.Printf("Processing %d pages of PDF %s in %d batches (concurrency: %d)",
		len(pages), filename, len(batches), c.concurrency)

	// Process batches concurrently with semaphore.
	semaphore := make(chan struct{}, c.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allResults []*PageResult
	var successCount, failCount int

	for _, b := range batches {
		wg.Add(1)
		go func(batch batchInfo) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			batchName := fmt.Sprintf("%s pages%d-%d", sanitizeDocumentName(filename), batch.startPage, batch.endPage)

			// Retry with exponential backoff.
			var results []*PageResult
			var lastErr error
			for attempt := 0; attempt <= maxRetries; attempt++ {
				if attempt > 0 {
					delay := retryBaseDelay * time.Duration(1<<uint(attempt-1))
					log.Printf("Retrying batch %s (attempt %d/%d) after %v", batchName, attempt+1, maxRetries+1, delay)
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						lastErr = ctx.Err()
						break
					}
				}

				results, lastErr = c.callGemini(ctx, batch.data, batchName)
				if lastErr == nil {
					break
				}
			}

			mu.Lock()
			defer mu.Unlock()

			if lastErr != nil {
				log.Printf("Warning: failed to OCR batch %s of PDF %s after %d attempts: %v, skipping %d pages",
					batchName, filename, maxRetries+1, lastErr, len(batch.pages))
				failCount += len(batch.pages)
				return
			}

			// Map page indices back to the original page numbers.
			for _, r := range results {
				if r.PageIndex >= 1 && r.PageIndex <= len(batch.pages) {
					r.PageIndex = batch.pages[r.PageIndex-1]
				} else if len(batch.pages) == 1 {
					r.PageIndex = batch.pages[0]
				}
			}
			allResults = append(allResults, results...)
			successCount += len(batch.pages)
		}(b)
	}

	wg.Wait()

	log.Printf("PDF %s OCR complete: %d/%d pages succeeded, %d failed",
		filename, successCount, len(pages), failCount)

	// Sort by page index to maintain document order.
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].PageIndex < allResults[j].PageIndex
	})

	return allResults, nil
}

// callGemini sends a PDF to Gemini API and parses the response.
func (c *GeminiOCRClient) callGemini(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	parts := []*genai.Part{
		{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: pdfData}},
		{Text: ocrPrompt},
	}
	contents := []*genai.Content{{Parts: parts}}

	temp := float32(0.0)
	config := &genai.GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: c.maxTokens,
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	log.Printf("Calling Gemini API for PDF: %s (model: %s)", filename, c.model)
	result, err := c.genaiClient.Models.GenerateContent(callCtx, c.model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("Gemini API call failed for %s: %w", filename, err)
	}

	responseText := extractTextFromGeminiResponse(result)
	if responseText == "" {
		return nil, fmt.Errorf("empty response from Gemini API for %s", filename)
	}

	results, err := parsePageResults(responseText)
	if err != nil {
		log.Printf("Warning: failed to parse JSON response for %s, falling back to single page: %v", filename, err)
		// Fallback: treat entire response as a single page result.
		return []*PageResult{
			{PageIndex: 1, Text: responseText},
		}, nil
	}


	log.Printf("Successfully extracted %d pages from PDF: %s", len(results), filename)
	return results, nil
}

// extractTextFromGeminiResponse extracts text content from a Gemini GenerateContentResponse.
func extractTextFromGeminiResponse(result *genai.GenerateContentResponse) string {
	if result == nil || len(result.Candidates) == 0 {
		return ""
	}

	candidate := result.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return ""
	}

	var texts []string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" && !part.Thought {
			texts = append(texts, part.Text)
		}
	}

	return strings.Join(texts, "\n")
}

// ValidateConnection checks if the Gemini service is accessible.
func (c *GeminiOCRClient) ValidateConnection(ctx context.Context) error {
	config := &genai.GenerateContentConfig{
		MaxOutputTokens: 10,
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := c.genaiClient.Models.GenerateContent(callCtx, c.model, genai.Text("test"), config)
	if err != nil {
		return fmt.Errorf("Gemini connection validation failed: %w", err)
	}
	return nil
}
