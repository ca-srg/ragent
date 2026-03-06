package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpumodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	// defaultModel is the default OCR model ID.
	defaultModel = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	// maxPDFSizeBytes is the maximum PDF size before splitting (4.5MB).
	maxPDFSizeBytes = 4_500_000
	// ocrPrompt is the hardcoded OCR + metadata extraction prompt.
	ocrPrompt = `You are a precise OCR system. Extract ALL text from each page of this PDF document.
Return the result as a JSON array where each element represents one page.
For the first page, also extract: title, category, and tags.
For subsequent pages, title/category/tags can be empty.
Structure: [{"page_index": 1, "text": "...", "title": "...", "category": "...", "tags": [...], "summary": "..."}]
Return ONLY the JSON array, no other text.`
)

// BedrockOCRClient implements the OCRClient interface using AWS Bedrock Converse API.
type BedrockOCRClient struct {
	bedrockClient *bedrockruntime.Client
	model         string
	timeout       time.Duration
	maxPages      int
}

// NewBedrockOCRClient creates a new BedrockOCRClient.
func NewBedrockOCRClient(awsConfig aws.Config, model string, timeout time.Duration, maxPages int) (*BedrockOCRClient, error) {
	if model == "" {
		model = defaultModel
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if maxPages <= 0 {
		maxPages = 100
	}

	return &BedrockOCRClient{
		bedrockClient: bedrockruntime.NewFromConfig(awsConfig),
		model:         model,
		timeout:       timeout,
		maxPages:      maxPages,
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

	return c.callBedrock(ctx, pdfData, filename)
}

// extractPagesFromLargePDF splits a large PDF into individual pages and processes each.
// It uses pdfcpu to parse the PDF and extract individual pages as separate PDF bytes.
func (c *BedrockOCRClient) extractPagesFromLargePDF(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	conf := pdfcpumodel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpumodel.ValidationRelaxed

	pdfCtx, err := pdfcpuapi.ReadAndValidate(bytes.NewReader(pdfData), conf)
	if err != nil {
		log.Printf("Warning: failed to parse PDF %s: %v, falling back to direct processing", filename, err)
		// Fallback: process the whole PDF even if large.
		return c.callBedrock(ctx, pdfData, filename)
	}

	pageCount := pdfCtx.PageCount
	if pageCount > c.maxPages {
		log.Printf("Warning: PDF %s has %d pages, truncating to %d", filename, pageCount, c.maxPages)
		pageCount = c.maxPages
	}

	var allResults []*PageResult
	for i := 1; i <= pageCount; i++ {
		pageReader, err := pdfcpuapi.ExtractPage(pdfCtx, i)
		if err != nil {
			log.Printf("Warning: failed to extract page %d of PDF %s: %v, skipping", i, filename, err)
			continue
		}

		pageData, err := io.ReadAll(pageReader)
		if err != nil {
			log.Printf("Warning: failed to read page %d of PDF %s: %v, skipping", i, filename, err)
			continue
		}

		pageName := fmt.Sprintf("%s page%d", sanitizeDocumentName(filename), i)
		results, err := c.callBedrock(ctx, pageData, pageName)
		if err != nil {
			log.Printf("Warning: failed to OCR page %d of PDF %s: %v, skipping", i, filename, err)
			continue
		}

		// Override page index to reflect position in the full document.
		for _, r := range results {
			r.PageIndex = i
		}
		allResults = append(allResults, results...)
	}

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
			MaxTokens:   aws.Int32(4096),
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	log.Printf("Calling Bedrock Converse API for PDF: %s (model: %s)", filename, c.model)
	output, err := c.bedrockClient.Converse(callCtx, input)
	if err != nil {
		return nil, fmt.Errorf("Bedrock Converse API call failed for %s: %w", filename, err)
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

	if len(results) > c.maxPages {
		log.Printf("Warning: PDF %s has %d pages, truncating to %d", filename, len(results), c.maxPages)
		results = results[:c.maxPages]
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
		return fmt.Errorf("Bedrock connection validation failed: %w", err)
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
func parsePageResults(responseText string) ([]*PageResult, error) {
	// Find JSON array in the response (handle potential surrounding text).
	start := strings.Index(responseText, "[")
	end := strings.LastIndex(responseText, "]")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON array found in response")
	}

	jsonStr := responseText[start : end+1]

	var results []*PageResult
	if err := json.Unmarshal([]byte(jsonStr), &results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal page results: %w", err)
	}

	return results, nil
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
