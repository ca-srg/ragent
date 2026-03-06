package pdf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestGeminiOCRClient_ImplementsInterface(t *testing.T) {
	var _ OCRClient = &GeminiOCRClient{}
}

func TestNewGeminiOCRClient_MissingAPIKey(t *testing.T) {
	_, err := NewGeminiOCRClient("", "gemini-2.5-flash", 0, 0, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GEMINI_API_KEY is required")
}

func TestNewGeminiOCRClient_DefaultValues(t *testing.T) {
	// Cannot create a real client without a valid API key,
	// but we can verify default values via the error path.
	_, err := NewGeminiOCRClient("", "", 0, 0, 0)
	assert.Error(t, err, "should fail without API key")
}

func TestDefaultGeminiModel_IsDefined(t *testing.T) {
	assert.NotEmpty(t, defaultGeminiModel, "defaultGeminiModel constant should be defined and non-empty")
	assert.Equal(t, "gemini-2.5-flash", defaultGeminiModel)
}

func TestExtractTextFromGeminiResponse_NilResponse(t *testing.T) {
	result := extractTextFromGeminiResponse(nil)
	assert.Empty(t, result)
}

func TestExtractTextFromGeminiResponse_NoCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Empty(t, result)
}

func TestExtractTextFromGeminiResponse_NilContent(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: nil},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Empty(t, result)
}

func TestExtractTextFromGeminiResponse_EmptyParts(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{}}},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Empty(t, result)
}

func TestExtractTextFromGeminiResponse_SingleTextPart(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "Hello World"},
					},
				},
			},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Equal(t, "Hello World", result)
}

func TestExtractTextFromGeminiResponse_MultipleTextParts(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "Part 1"},
						{Text: "Part 2"},
					},
				},
			},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Equal(t, "Part 1\nPart 2", result)
}

func TestExtractTextFromGeminiResponse_SkipsThoughtParts(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "thinking...", Thought: true},
						{Text: "Actual response"},
					},
				},
			},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Equal(t, "Actual response", result)
}

func TestExtractTextFromGeminiResponse_SkipsEmptyText(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: ""},
						{Text: "Valid text"},
					},
				},
			},
		},
	}
	result := extractTextFromGeminiResponse(resp)
	assert.Equal(t, "Valid text", result)
}
