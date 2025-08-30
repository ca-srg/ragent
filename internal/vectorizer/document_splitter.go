package vectorizer

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DocumentSplitter handles document chunking for large texts
type DocumentSplitter struct {
	MaxTokens     int     // Maximum tokens per chunk (default: 7000 for safety)
	OverlapTokens int     // Number of overlapping tokens between chunks (default: 200)
	TokensPerChar float64 // Estimated tokens per character for Japanese text (default: 0.7)
}

// NewDocumentSplitter creates a new document splitter with default settings
func NewDocumentSplitter() *DocumentSplitter {
	return &DocumentSplitter{
		MaxTokens:     7000, // Safe limit for 8192 token model
		OverlapTokens: 200,  // Overlap for context preservation
		TokensPerChar: 0.7,  // Japanese text typically has ~0.7 tokens per character
	}
}

// ChunkedDocument represents a chunk of a larger document
type ChunkedDocument struct {
	Content     string            // The chunk content
	ChunkIndex  int               // Index of this chunk (0-based)
	TotalChunks int               // Total number of chunks
	OriginalID  string            // ID of the original document
	Metadata    map[string]string // Additional metadata
}

// EstimateTokenCount estimates the token count for a given text
func (ds *DocumentSplitter) EstimateTokenCount(text string) int {
	// For Japanese text, we use character count * TokensPerChar
	// This is a rough estimate, but works well for Japanese business documents
	charCount := utf8.RuneCountInString(text)
	return int(float64(charCount) * ds.TokensPerChar)
}

// ShouldSplit determines if a document needs to be split
func (ds *DocumentSplitter) ShouldSplit(text string) bool {
	estimatedTokens := ds.EstimateTokenCount(text)
	return estimatedTokens > ds.MaxTokens
}

// SplitDocument splits a document into chunks if necessary
func (ds *DocumentSplitter) SplitDocument(text string, documentID string) ([]*ChunkedDocument, error) {
	if text == "" {
		return nil, fmt.Errorf("empty document text")
	}

	// Check if splitting is needed
	if !ds.ShouldSplit(text) {
		// Return single chunk
		return []*ChunkedDocument{
			{
				Content:     text,
				ChunkIndex:  0,
				TotalChunks: 1,
				OriginalID:  documentID,
				Metadata:    make(map[string]string),
			},
		}, nil
	}

	// Calculate characters per chunk based on token limits
	maxCharsPerChunk := int(float64(ds.MaxTokens) / ds.TokensPerChar)
	overlapChars := int(float64(ds.OverlapTokens) / ds.TokensPerChar)

	chunks := ds.splitByCharacters(text, maxCharsPerChunk, overlapChars)

	// Create ChunkedDocument objects
	result := make([]*ChunkedDocument, len(chunks))
	for i, chunk := range chunks {
		result[i] = &ChunkedDocument{
			Content:     chunk,
			ChunkIndex:  i,
			TotalChunks: len(chunks),
			OriginalID:  documentID,
			Metadata: map[string]string{
				"chunk_index":  fmt.Sprintf("%d", i),
				"total_chunks": fmt.Sprintf("%d", len(chunks)),
				"original_id":  documentID,
			},
		}
	}

	return result, nil
}

// splitByCharacters splits text into chunks with overlap
func (ds *DocumentSplitter) splitByCharacters(text string, maxChars, overlapChars int) []string {
	var chunks []string

	// Convert to rune slice for proper Unicode handling
	runes := []rune(text)
	totalRunes := len(runes)

	if totalRunes <= maxChars {
		return []string{text}
	}

	start := 0
	for start < totalRunes {
		end := start + maxChars
		if end > totalRunes {
			end = totalRunes
		}

		// Try to find a natural break point (paragraph or sentence end)
		if end < totalRunes {
			// Look for paragraph break first
			breakPoint := ds.findBreakPoint(runes[start:end], []string{"\n\n", "。\n", ".\n"})
			if breakPoint > 0 {
				end = start + breakPoint
			} else {
				// Look for sentence break
				breakPoint = ds.findBreakPoint(runes[start:end], []string{"。", ".", "！", "？", "!\n", "?\n"})
				if breakPoint > 0 {
					end = start + breakPoint
				}
			}
		}

		chunks = append(chunks, string(runes[start:end]))

		// Move to next chunk with overlap
		if end >= totalRunes {
			break
		}

		// Calculate next start position with overlap
		nextStart := end - overlapChars
		if nextStart <= start {
			// Ensure we make progress even with small chunks
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}

// findBreakPoint finds the last occurrence of any delimiter in the text
func (ds *DocumentSplitter) findBreakPoint(runes []rune, delimiters []string) int {
	text := string(runes)
	bestPos := -1

	for _, delimiter := range delimiters {
		pos := strings.LastIndex(text, delimiter)
		if pos > bestPos {
			bestPos = pos + len(delimiter)
		}
	}

	if bestPos > 0 {
		// Convert byte position back to rune position
		return utf8.RuneCountInString(text[:bestPos])
	}

	return 0
}

// GenerateChunkID generates a unique ID for a chunk
func (ds *DocumentSplitter) GenerateChunkID(originalID string, chunkIndex int) string {
	return fmt.Sprintf("%s_chunk_%d", originalID, chunkIndex)
}

// MergeChunks merges multiple chunks back into original text (for testing/validation)
func (ds *DocumentSplitter) MergeChunks(chunks []*ChunkedDocument) string {
	if len(chunks) == 0 {
		return ""
	}

	// Sort chunks by index and merge
	contents := make([]string, len(chunks))
	for _, chunk := range chunks {
		if chunk.ChunkIndex < len(contents) {
			contents[chunk.ChunkIndex] = chunk.Content
		}
	}

	// Remove overlapping parts
	if ds.OverlapTokens > 0 && len(contents) > 1 {
		overlapChars := int(float64(ds.OverlapTokens) / ds.TokensPerChar)
		for i := 1; i < len(contents); i++ {
			// Remove overlap from the beginning of each chunk (except the first)
			if len(contents[i]) > overlapChars {
				contents[i] = contents[i][overlapChars:]
			}
		}
	}

	return strings.Join(contents, "")
}
