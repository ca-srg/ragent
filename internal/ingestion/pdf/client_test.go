package pdf

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockOCRClient implements OCRClient for testing.
type MockOCRClient struct {
	ExtractPagesFunc       func(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error)
	ValidateConnectionFunc func(ctx context.Context) error
}

func (m *MockOCRClient) ExtractPages(ctx context.Context, pdfData []byte, filename string) ([]*PageResult, error) {
	if m.ExtractPagesFunc != nil {
		return m.ExtractPagesFunc(ctx, pdfData, filename)
	}
	return nil, nil
}

func (m *MockOCRClient) ValidateConnection(ctx context.Context) error {
	if m.ValidateConnectionFunc != nil {
		return m.ValidateConnectionFunc(ctx)
	}
	return nil
}

func TestMockOCRClient_ImplementsInterface(t *testing.T) {
	var _ OCRClient = &MockOCRClient{}
}

func TestDefaultModel_IsDefined(t *testing.T) {
	assert.NotEmpty(t, defaultModel, "defaultModel constant should be defined and non-empty")
}

func TestSanitizeDocumentName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple filename",
			input:    "report",
			expected: "report",
		},
		{
			name:     "filename with extension",
			input:    "report.pdf",
			expected: "report-pdf",
		},
		{
			name:     "filename with path separators",
			input:    "/path/to/report.pdf",
			expected: "-path-to-report-pdf",
		},
		{
			name:     "filename with underscores",
			input:    "my_report_2024.pdf",
			expected: "my-report-2024-pdf",
		},
		{
			name:     "filename with spaces",
			input:    "my report",
			expected: "my report",
		},
		{
			name:     "consecutive spaces collapsed",
			input:    "my   report",
			expected: "my report",
		},
		{
			name:     "empty string returns document",
			input:    "",
			expected: "document",
		},
		{
			name:     "brackets and parens preserved",
			input:    "report[1](a)",
			expected: "report[1](a)",
		},
		{
			name:     "hyphens preserved",
			input:    "my-report",
			expected: "my-report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeDocumentName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeDocumentName_TruncatesLongNames(t *testing.T) {
	longName := ""
	for i := 0; i < 120; i++ {
		longName += "a"
	}
	result := sanitizeDocumentName(longName)
	assert.LessOrEqual(t, len(result), 100, "sanitized name should be at most 100 characters")
}

func TestParsePageResults_ValidJSON(t *testing.T) {
	input := `[{"page_index": 1, "text": "Hello", "title": "T", "category": "C", "tags": ["a"], "summary": "S"}]`
	results, err := parsePageResults(input)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, results[0].PageIndex)
	assert.Equal(t, "Hello", results[0].Text)
	assert.Equal(t, "T", results[0].Title)
	assert.Equal(t, "C", results[0].Category)
	assert.Equal(t, []string{"a"}, results[0].Tags)
	assert.Equal(t, "S", results[0].Summary)
}

func TestParsePageResults_MultiplePages(t *testing.T) {
	input := `[{"page_index": 1, "text": "Page 1"}, {"page_index": 2, "text": "Page 2"}]`
	results, err := parsePageResults(input)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "Page 1", results[0].Text)
	assert.Equal(t, "Page 2", results[1].Text)
}

func TestParsePageResults_SurroundingText(t *testing.T) {
	input := `Here is the result: [{"page_index": 1, "text": "content"}] end of response`
	results, err := parsePageResults(input)
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "content", results[0].Text)
}

func TestParsePageResults_NoJSONArray(t *testing.T) {
	input := `This is not JSON at all`
	_, err := parsePageResults(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON array found")
}

func TestParsePageResults_InvalidJSON(t *testing.T) {
	input := `[{"page_index": broken}]`
	_, err := parsePageResults(input)
	assert.Error(t, err)
}

func TestParsePageResults_EmptyArray(t *testing.T) {
	input := `[]`
	results, err := parsePageResults(input)
	assert.NoError(t, err)
	assert.Empty(t, results)
}
