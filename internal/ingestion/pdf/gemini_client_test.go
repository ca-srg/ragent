package pdf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestGeminiOCRClient_ImplementsInterface(t *testing.T) {
	var _ OCRClient = &GeminiOCRClient{}
}

func TestNewGeminiOCRClient_MissingCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	_, err := NewGeminiOCRClient("", "", "", "gemini-2.5-flash", 0, 0, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create Gemini client with ADC")
}

func TestNewGeminiOCRClient_DefaultValues(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	_, err := NewGeminiOCRClient("", "", "", "", 0, 0, 0)
	assert.Error(t, err, "should fail without API key or ADC credentials")
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
