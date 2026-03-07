package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpumodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	// defaultModel is the default OCR model ID.
	defaultModel = "anthropic.claude-sonnet-4-6"
	// maxPDFSizeBytes is the maximum PDF size before splitting (4.5MB).
	maxPDFSizeBytes = 4_500_000
	// maxPagesForSingleCall is the maximum page count for processing a PDF in a single API call.
	// PDFs with more pages are split into batches to avoid output token truncation.
	maxPagesForSingleCall = 20
	// maxRetries is the number of retry attempts for failed API calls.
	maxRetries = 3
	// retryBaseDelay is the base delay for exponential backoff retries.
	retryBaseDelay = 2 * time.Second
	// ocrPrompt is the hardcoded OCR + metadata extraction prompt.
	ocrPrompt = `You are a precise OCR system. Extract ALL text from each page of this PDF document.
Return the result as a JSON array where each element represents one page.
For EVERY page, extract these metadata fields from the page content:
- title: the document title or section heading visible on the page (not just a page number)
- category: the document type or subject area (e.g. "仕様書", "設計書", "マニュアル")
- tags: relevant keywords from the page content
- summary: a brief one-line summary of the page content
- author: the document author or creator if visible on the page (e.g. from headers, footers, or cover page)
If a metadata field cannot be determined from the page content, use an empty string or empty array.
Structure: [{"page_index": 1, "text": "...", "title": "...", "category": "...", "tags": [...], "summary": "...", "author": "..."}]
Return ONLY the JSON array, no markdown code fences, no other text.`
)

// BedrockOCRClient implements the OCRClient interface using AWS Bedrock Converse API.
type BedrockOCRClient struct {
	bedrockClient *bedrockruntime.Client
	model         string
	timeout       time.Duration
	maxTokens     int32
	concurrency   int
}

// NewBedrockOCRClient creates a new BedrockOCRClient.
func NewBedrockOCRClient(awsConfig aws.Config, model string, timeout time.Duration, maxTokens int, concurrency int) (*BedrockOCRClient, error) {
	if model == "" {
		model = defaultModel
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

	return &BedrockOCRClient{
		bedrockClient: bedrockruntime.NewFromConfig(awsConfig),
		model:         model,
		timeout:       timeout,
		maxTokens:     int32(maxTokens),
		concurrency:   concurrency,
	}, nil
}

// ExtractPages performs OCR on a PDF document and returns per-page results.
func (c *BedrockOCRClient) ExtractPages(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	if len(pdfData) == 0 {
		return nil, fmt.Errorf("PDF data is empty")
	}

	// Split large PDFs into individual pages before sending to Bedrock.
	if len(pdfData) > maxPDFSizeBytes {
		log.Printf("PDF %s is large (%d bytes), splitting into pages", filename, len(pdfData))
		return c.extractPagesFromLargePDF(ctx, pdfData, filename)
	}

	// Split PDFs with many pages to avoid output token truncation.
	if pageCount := countPDFPages(pdfData); pageCount > maxPagesForSingleCall {
		log.Printf("PDF %s has %d pages (> %d), splitting into batches to avoid token truncation", filename, pageCount, maxPagesForSingleCall)
		return c.extractPagesFromLargePDF(ctx, pdfData, filename)
	}

	return c.callBedrock(ctx, pdfData, filename)
}

// extractPagesFromLargePDF splits a large PDF into batches of pages and processes each batch.
// It uses pdfcpu to extract individual pages, merges them into batches, and sends each batch
// to the OCR API with retry logic for resilience against rate limiting.
func (c *BedrockOCRClient) extractPagesFromLargePDF(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	conf := pdfcpumodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpumodel.ValidationRelaxed

	pdfCtx, err := pdfcpuapi.ReadAndValidate(bytes.NewReader(pdfData), conf)
	if err != nil {
		log.Printf("Warning: failed to parse PDF %s: %v, falling back to direct processing", filename, err)
		return c.callBedrock(ctx, pdfData, filename)
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
					}
					if ctx.Err() != nil {
						lastErr = ctx.Err()
						break
					}
				}

				results, lastErr = c.callBedrock(ctx, batch.data, batchName)
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

// callBedrock sends a PDF to Bedrock Converse API and parses the response.
func (c *BedrockOCRClient) callBedrock(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	docName := sanitizeDocumentName(filename)

	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(c.model),
		Messages: []brtypes.Message{
			{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberDocument{
						Value: brtypes.DocumentBlock{
							Format: brtypes.DocumentFormatPdf,
							Name:   aws.String(docName),
							Source: &brtypes.DocumentSourceMemberBytes{
								Value: pdfData,
							},
						},
					},
					&brtypes.ContentBlockMemberText{
						Value: ocrPrompt,
					},
				},
			},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{
			Temperature: aws.Float32(0.0),
			MaxTokens:   aws.Int32(c.maxTokens),
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	log.Printf("Calling Bedrock Converse API for PDF: %s (model: %s)", filename, c.model)
	output, err := c.bedrockClient.Converse(callCtx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock Converse API call failed for %s: %w", filename, err)
	}

	responseText, err := extractTextFromConverseOutput(output)
	if err != nil {
		return nil, fmt.Errorf("failed to extract text from Bedrock response for %s: %w", filename, err)
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

// ValidateConnection checks if the Bedrock service is accessible.
func (c *BedrockOCRClient) ValidateConnection(ctx context.Context) error {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(c.model),
		Messages: []brtypes.Message{
			{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{
						Value: "test",
					},
				},
			},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: aws.Int32(10),
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := c.bedrockClient.Converse(callCtx, input)
	if err != nil {
		return fmt.Errorf("bedrock connection validation failed: %w", err)
	}
	return nil
}

// extractTextFromConverseOutput extracts text content from a Converse API response.
func extractTextFromConverseOutput(output *bedrockruntime.ConverseOutput) (string, error) {
	if output == nil {
		return "", fmt.Errorf("empty Converse output")
	}

	msgOutput, ok := output.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("unexpected Converse output type")
	}

	var texts []string
	for _, block := range msgOutput.Value.Content {
		if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
			texts = append(texts, textBlock.Value)
		}
	}

	if len(texts) == 0 {
		return "", fmt.Errorf("no text content in Converse response")
	}

	return strings.Join(texts, "\n"), nil
}

// parsePageResults parses the JSON array response from the OCR prompt.
// It strips markdown code fences, uses json.Decoder to read the first complete
// JSON value, and attempts partial recovery when the response is truncated.
func parsePageResults(responseText string) ([]*PageResult, error) {
	// Strip markdown code fences (```json ... ``` or ```...```).
	text := stripMarkdownCodeFences(responseText)

	// Find JSON array in the response (handle potential surrounding text).
	start := strings.Index(text, "[")
	if start == -1 {
		return nil, fmt.Errorf("no JSON array found in response")
	}

	jsonStr := text[start:]

	// First attempt: decode complete JSON.
	decoder := json.NewDecoder(strings.NewReader(jsonStr))
	var results []*PageResult
	if err := decoder.Decode(&results); err != nil {
		// Second attempt: response may be truncated by token limit.
		// Try to recover by closing the JSON array at the last valid element.
		recovered := recoverTruncatedJSON(jsonStr)
		if recovered != "" {
			var recoveredResults []*PageResult
			if err2 := json.Unmarshal([]byte(recovered), &recoveredResults); err2 == nil && len(recoveredResults) > 0 {
				log.Printf("Warning: recovered %d pages from truncated OCR response", len(recoveredResults))
				return recoveredResults, nil
			}
		}
		return nil, fmt.Errorf("failed to unmarshal page results: %w", err)
	}

	return results, nil
}

// stripMarkdownCodeFences removes markdown code fences from an LLM response.
// Handles patterns like: ```json\n...\n``` or ```\n...\n```
func stripMarkdownCodeFences(text string) string {
	// Remove leading code fence (```json or ```)
	if idx := strings.Index(text, "```"); idx != -1 {
		after := text[idx+3:]
		// Skip optional language identifier (e.g., "json")
		if nl := strings.Index(after, "\n"); nl != -1 {
			text = after[nl+1:]
		}
	}
	// Remove trailing code fence
	if idx := strings.LastIndex(text, "```"); idx != -1 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

// recoverTruncatedJSON attempts to fix a truncated JSON array by finding the last
// complete object and closing the array. This handles cases where LLM output was
// cut off by the max_tokens limit.
func recoverTruncatedJSON(jsonStr string) string {
	// Find the last complete object by looking for the last "}," or "}" followed by incomplete data.
	lastCloseBrace := strings.LastIndex(jsonStr, "}")
	if lastCloseBrace == -1 {
		return ""
	}

	// Check if there's a valid array start
	if !strings.HasPrefix(strings.TrimSpace(jsonStr), "[") {
		return ""
	}

	// Truncate at the last complete object and close the array
	candidate := strings.TrimSpace(jsonStr[:lastCloseBrace+1])
	// Remove trailing comma if present
	candidate = strings.TrimRight(candidate, " \t\n\r,")
	// Ensure it ends with } (the last complete object)
	if !strings.HasSuffix(candidate, "}") {
		return ""
	}
	return candidate + "]"
}

// sanitizeDocumentName creates a valid document name for Bedrock API.
// The name may only contain alphanumeric characters, single whitespace, hyphens,
// parentheses, and square brackets.
func sanitizeDocumentName(filename string) string {
	var sb strings.Builder
	prevWasSpace := false

	for _, ch := range filename {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
			sb.WriteRune(ch)
			prevWasSpace = false
		case ch == '-' || ch == '(' || ch == ')' || ch == '[' || ch == ']':
			sb.WriteRune(ch)
			prevWasSpace = false
		case ch == ' ':
			// Allow single whitespace, skip consecutive spaces.
			if !prevWasSpace {
				sb.WriteRune(' ')
				prevWasSpace = true
			}
		default:
			// Replace invalid characters (e.g. / . _) with a hyphen.
			if !prevWasSpace {
				sb.WriteRune('-')
				prevWasSpace = false
			}
		}
	}

	name := strings.TrimSpace(sb.String())
	if len(name) > 100 {
		name = name[:100]
	}
	if name == "" {
		name = "document"
	}
	return name
}

// countPDFPages returns the page count of a PDF without fully processing it.
// Returns 0 if the PDF cannot be parsed (caller should fall through to direct processing).
func countPDFPages(pdfData []byte) int {
	conf := pdfcpumodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpumodel.ValidationRelaxed
	pdfCtx, err := pdfcpuapi.ReadAndValidate(bytes.NewReader(pdfData), conf)
	if err != nil {
		return 0
	}
	return pdfCtx.PageCount
}

// mergePagePDFs merges multiple individual page PDFs into a single PDF.
// Returns the merged PDF bytes, or an error if merging fails.
func mergePagePDFs(pagePDFs [][]byte) ([]byte, error) {
	if len(pagePDFs) == 0 {
		return nil, fmt.Errorf("no page PDFs to merge")
	}
	if len(pagePDFs) == 1 {
		return pagePDFs[0], nil
	}

	readers := make([]io.ReadSeeker, len(pagePDFs))
	for i, pd := range pagePDFs {
		readers[i] = bytes.NewReader(pd)
	}

	var buf bytes.Buffer
	conf := pdfcpumodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpumodel.ValidationRelaxed
	if err := pdfcpuapi.MergeRaw(readers, &buf, false, conf); err != nil {
		return nil, fmt.Errorf("failed to merge page PDFs: %w", err)
	}
	return buf.Bytes(), nil
}

// batchPageData groups extracted page data into batches of the given size.
type batchInfo struct {
	startPage int    // 1-based page index of first page in batch
	endPage   int    // 1-based page index of last page in batch
	data      []byte // merged PDF bytes for this batch
	pages     []int  // original page indices in this batch
}
