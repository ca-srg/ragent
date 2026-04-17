package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/ca-srg/ragent/internal/pkg/opensearch"
)

func TestSanitizeSourceFields(t *testing.T) {
	tests := []struct {
		name     string
		source   map[string]interface{}
		wantKeys []string
	}{
		{
			name: "removes embedding when present",
			source: map[string]interface{}{
				"title":     "doc1",
				"content":   "hello",
				"embedding": []float64{0.1, 0.2, 0.3},
			},
			wantKeys: []string{"title", "content"},
		},
		{
			name: "no-op when embedding absent",
			source: map[string]interface{}{
				"title":   "doc2",
				"content": "world",
			},
			wantKeys: []string{"title", "content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitizeSourceFields(tt.source)

			if _, ok := tt.source["embedding"]; ok {
				t.Fatalf("embedding should have been removed")
			}

			for _, key := range tt.wantKeys {
				if _, ok := tt.source[key]; !ok {
					t.Fatalf("expected key %q to be present", key)
				}
			}
		})
	}
}

func TestSanitizeSourceFieldsPreservesOtherFields(t *testing.T) {
	source := map[string]interface{}{
		"title":     "Document Title",
		"content":   "Document content",
		"category":  "tech",
		"tags":      []string{"go", "search"},
		"reference": "https://example.com",
		"secret":    false,
		"embedding": []float64{0.1, 0.2},
	}

	sanitizeSourceFields(source)

	if _, ok := source["embedding"]; ok {
		t.Fatalf("embedding should have been removed")
	}

	preserved := []string{"title", "content", "category", "tags", "reference", "secret"}
	for _, key := range preserved {
		if _, ok := source[key]; !ok {
			t.Fatalf("expected field %q to be preserved", key)
		}
	}
}

func TestConvertToMCPResponseStripsEmbedding(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}

	// Create a document with embedding in its source JSON
	sourceJSON := `{"title":"Test Doc","content":"Hello world","embedding":[0.1,0.2,0.3],"category":"tech"}`

	request := &HybridSearchRequest{
		Query:           "test",
		IncludeMetadata: true,
	}

	result := &opensearch.HybridSearchResult{
		FusionResult: &opensearch.FusionResult{
			Documents: []opensearch.ScoredDoc{
				{
					ID:         "doc-1",
					FusedScore: 0.95,
					Source:     json.RawMessage(sourceJSON),
				},
			},
			TotalHits: 1,
		},
	}

	response := adapter.convertToMCPResponse(request, result, nil, nil)

	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}

	metadata := response.Results[0].Metadata
	if metadata == nil {
		t.Fatalf("expected metadata to be present when IncludeMetadata=true")
	}

	// embedding should be stripped
	if _, ok := metadata["embedding"]; ok {
		t.Fatalf("embedding should have been stripped from metadata")
	}

	// other fields should be preserved
	if title, ok := metadata["title"].(string); !ok || title != "Test Doc" {
		t.Fatalf("expected title to be preserved, got %v", metadata["title"])
	}
	if category, ok := metadata["category"].(string); !ok || category != "tech" {
		t.Fatalf("expected category to be preserved, got %v", metadata["category"])
	}
}
