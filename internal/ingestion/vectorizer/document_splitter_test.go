package vectorizer

import (
	"strings"
	"testing"
)

func TestDocumentSplitter_EstimateTokenCount(t *testing.T) {
	ds := NewDocumentSplitter()

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "Empty text",
			text:     "",
			expected: 0,
		},
		{
			name:     "ASCII text",
			text:     "Hello World",
			expected: 7, // 11 chars * 0.7
		},
		{
			name:     "Japanese text",
			text:     "こんにちは世界",
			expected: 4, // 7 chars * 0.7
		},
		{
			name:     "Mixed text",
			text:     "Hello こんにちは",
			expected: 7, // 11 chars * 0.7
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ds.EstimateTokenCount(tt.text)
			if got != tt.expected {
				t.Errorf("EstimateTokenCount() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDocumentSplitter_ShouldSplit(t *testing.T) {
	ds := NewDocumentSplitter()
	ds.MaxTokens = 100

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "Small text",
			text:     "Short text",
			expected: false,
		},
		{
			name:     "Large text",
			text:     strings.Repeat("あ", 200), // 200 chars * 0.7 = 140 tokens > 100
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ds.ShouldSplit(tt.text)
			if got != tt.expected {
				t.Errorf("ShouldSplit() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDocumentSplitter_SplitDocument(t *testing.T) {
	ds := NewDocumentSplitter()
	ds.MaxTokens = 50
	ds.OverlapTokens = 10

	tests := []struct {
		name       string
		text       string
		documentID string
		wantChunks int
		wantError  bool
	}{
		{
			name:       "Empty text",
			text:       "",
			documentID: "doc1",
			wantError:  true,
		},
		{
			name:       "Single chunk",
			text:       "Short text",
			documentID: "doc2",
			wantChunks: 1,
			wantError:  false,
		},
		{
			name:       "Multiple chunks",
			text:       strings.Repeat("あいうえお。", 30), // 180 chars * 0.7 = 126 tokens
			documentID: "doc3",
			wantChunks: 4, // With MaxTokens=50 and overlap, this will result in 4 chunks
			wantError:  false,
		},
		{
			name:       "Text with natural breaks",
			text:       "第一段落。\n\n第二段落。\n\n第三段落。",
			documentID: "doc4",
			wantChunks: 1,
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ds.SplitDocument(tt.text, tt.documentID)

			if (err != nil) != tt.wantError {
				t.Errorf("SplitDocument() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError && len(got) != tt.wantChunks {
				t.Errorf("SplitDocument() got %v chunks, want %v", len(got), tt.wantChunks)
			}

			// Verify chunk metadata
			if !tt.wantError && len(got) > 0 {
				for i, chunk := range got {
					if chunk.ChunkIndex != i {
						t.Errorf("Chunk %d has wrong index: %d", i, chunk.ChunkIndex)
					}
					if chunk.TotalChunks != len(got) {
						t.Errorf("Chunk %d has wrong total: %d, want %d", i, chunk.TotalChunks, len(got))
					}
					if chunk.OriginalID != tt.documentID {
						t.Errorf("Chunk %d has wrong originalID: %s, want %s", i, chunk.OriginalID, tt.documentID)
					}
				}
			}
		})
	}
}

func TestDocumentSplitter_GenerateChunkID(t *testing.T) {
	ds := NewDocumentSplitter()

	tests := []struct {
		name       string
		originalID string
		chunkIndex int
		want       string
	}{
		{
			name:       "First chunk",
			originalID: "doc123",
			chunkIndex: 0,
			want:       "doc123_chunk_0",
		},
		{
			name:       "Second chunk",
			originalID: "doc456",
			chunkIndex: 1,
			want:       "doc456_chunk_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ds.GenerateChunkID(tt.originalID, tt.chunkIndex)
			if got != tt.want {
				t.Errorf("GenerateChunkID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDocumentSplitter_MergeChunks(t *testing.T) {
	ds := NewDocumentSplitter()
	ds.OverlapTokens = 0 // No overlap for simple testing

	originalText := "First chunk. Second chunk. Third chunk."
	chunks := []*ChunkedDocument{
		{
			Content:     "First chunk.",
			ChunkIndex:  0,
			TotalChunks: 3,
			OriginalID:  "doc1",
		},
		{
			Content:     " Second chunk.",
			ChunkIndex:  1,
			TotalChunks: 3,
			OriginalID:  "doc1",
		},
		{
			Content:     " Third chunk.",
			ChunkIndex:  2,
			TotalChunks: 3,
			OriginalID:  "doc1",
		},
	}

	merged := ds.MergeChunks(chunks)
	if merged != originalText {
		t.Errorf("MergeChunks() = %v, want %v", merged, originalText)
	}

	// Test empty chunks
	emptyMerged := ds.MergeChunks([]*ChunkedDocument{})
	if emptyMerged != "" {
		t.Errorf("MergeChunks() with empty chunks = %v, want empty string", emptyMerged)
	}
}
