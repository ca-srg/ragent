package vectorizer

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"unicode/utf8"
)

// OpenSearchDocument represents a document structure optimized for OpenSearch indexing
type OpenSearchDocument struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Content      string                 `json:"content"`
	ContentJa    string                 `json:"content_ja"` // Japanese processed content for kuromoji
	Body         string                 `json:"body"`       // Main text body
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	Author       string                 `json:"author"`
	Reference    string                 `json:"reference"`
	Source       string                 `json:"source"`
	FilePath     string                 `json:"file_path"`
	WordCount    int                    `json:"word_count"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	IndexedAt    time.Time              `json:"indexed_at"`
	Embedding    []float64              `json:"embedding"`
	ChunkIndex   *int                   `json:"chunk_index,omitempty"`  // Index of current chunk
	TotalChunks  *int                   `json:"total_chunks,omitempty"` // Total number of chunks
	CustomFields map[string]interface{} `json:"custom_fields,omitempty"`
}

// NewOpenSearchDocument creates a new OpenSearchDocument from VectorData
func NewOpenSearchDocument(vectorData *VectorData, contentJa string) *OpenSearchDocument {
	// Deep copy the embedding to avoid reference issues
	var embeddingCopy []float64
	if vectorData.Embedding != nil {
		embeddingCopy = make([]float64, len(vectorData.Embedding))
		copy(embeddingCopy, vectorData.Embedding)
	}

	// Ensure tags is not nil
	tags := vectorData.Metadata.Tags
	if tags == nil {
		tags = []string{}
	}

	return &OpenSearchDocument{
		ID:           vectorData.ID,
		Title:        vectorData.Metadata.Title,
		Content:      vectorData.Content,
		ContentJa:    contentJa,
		Body:         vectorData.Content,
		Category:     vectorData.Metadata.Category,
		Tags:         tags,
		Author:       vectorData.Metadata.Author,
		Reference:    vectorData.Metadata.Reference,
		Source:       vectorData.Metadata.Source,
		FilePath:     vectorData.Metadata.FilePath,
		WordCount:    vectorData.Metadata.WordCount,
		CreatedAt:    vectorData.Metadata.CreatedAt,
		UpdatedAt:    vectorData.Metadata.UpdatedAt,
		IndexedAt:    time.Now(),
		Embedding:    embeddingCopy,
		CustomFields: vectorData.Metadata.CustomFields,
	}
}

// Validate performs comprehensive validation of the OpenSearchDocument
func (doc *OpenSearchDocument) Validate() error {
	if doc == nil {
		return fmt.Errorf("document cannot be nil")
	}

	// ID validation
	if err := doc.validateID(); err != nil {
		return fmt.Errorf("ID validation failed: %w", err)
	}

	// Title validation
	if err := doc.validateTitle(); err != nil {
		return fmt.Errorf("title validation failed: %w", err)
	}

	// Content validation
	if err := doc.validateContent(); err != nil {
		return fmt.Errorf("content validation failed: %w", err)
	}

	// Embedding validation
	if err := doc.validateEmbedding(); err != nil {
		return fmt.Errorf("embedding validation failed: %w", err)
	}

	// Timestamp validation
	if err := doc.validateTimestamps(); err != nil {
		return fmt.Errorf("timestamp validation failed: %w", err)
	}

	return nil
}

// validateID validates the document ID
func (doc *OpenSearchDocument) validateID() error {
	if doc.ID == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	if len(doc.ID) > 512 {
		return fmt.Errorf("ID too long: %d characters (max 512)", len(doc.ID))
	}

	// Check for invalid characters
	if !utf8.ValidString(doc.ID) {
		return fmt.Errorf("ID contains invalid UTF-8 characters")
	}

	return nil
}

// validateTitle validates the document title
func (doc *OpenSearchDocument) validateTitle() error {
	if doc.Title == "" {
		return fmt.Errorf("title cannot be empty")
	}

	if len(doc.Title) > 1000 {
		return fmt.Errorf("title too long: %d characters (max 1000)", len(doc.Title))
	}

	if !utf8.ValidString(doc.Title) {
		return fmt.Errorf("title contains invalid UTF-8 characters")
	}

	return nil
}

// validateContent validates the document content
func (doc *OpenSearchDocument) validateContent() error {
	if doc.Content == "" {
		return fmt.Errorf("content cannot be empty")
	}

	// Content size limit (10MB)
	const maxContentSize = 10 * 1024 * 1024
	if len(doc.Content) > maxContentSize {
		return fmt.Errorf("content too large: %d bytes (max %d bytes)", len(doc.Content), maxContentSize)
	}

	if !utf8.ValidString(doc.Content) {
		return fmt.Errorf("content contains invalid UTF-8 characters")
	}

	// Validate Japanese processed content if present
	if doc.ContentJa != "" && !utf8.ValidString(doc.ContentJa) {
		return fmt.Errorf("japanese content contains invalid UTF-8 characters")
	}

	return nil
}

// validateEmbedding validates the embedding vector
func (doc *OpenSearchDocument) validateEmbedding() error {
	if doc.Embedding == nil {
		return fmt.Errorf("embedding vector cannot be nil")
	}
	if len(doc.Embedding) == 0 {
		return fmt.Errorf("embedding vector cannot be empty")
	}

	// Check for expected dimensions (common embedding sizes)
	validDimensions := map[int]bool{
		384:  true, // sentence-transformers/all-MiniLM-L6-v2
		512:  true, // sentence-transformers/all-mpnet-base-v2
		768:  true, // BERT-base
		1024: true, // BERT-large, Amazon Titan v2 (max dimension)
		1536: true, // OpenAI text-embedding-ada-002, Amazon Titan v1 (fixed)
		3072: true, // OpenAI text-embedding-3-large
	}

	if !validDimensions[len(doc.Embedding)] {
		return fmt.Errorf("embedding dimension %d is not in expected range (384, 512, 768, 1024, 1536, 3072)", len(doc.Embedding))
	}

	// Check for NaN or infinite values
	for i, val := range doc.Embedding {
		if val != val { // Check for NaN
			return fmt.Errorf("embedding contains NaN at index %d", i)
		}
		if val > 1e10 || val < -1e10 { // Check for extreme values
			return fmt.Errorf("embedding contains extreme value %f at index %d", val, i)
		}
	}

	return nil
}

// validateTimestamps validates timestamp fields
func (doc *OpenSearchDocument) validateTimestamps() error {
	now := time.Now()

	if doc.CreatedAt.IsZero() {
		return fmt.Errorf("created_at timestamp cannot be zero")
	}

	if doc.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at timestamp cannot be zero")
	}

	if doc.IndexedAt.IsZero() {
		return fmt.Errorf("indexed_at timestamp cannot be zero")
	}

	// Check for reasonable time ranges (not too far in the future)
	if doc.CreatedAt.After(now.Add(time.Hour)) {
		return fmt.Errorf("created_at timestamp is too far in the future: %v", doc.CreatedAt)
	}

	if doc.UpdatedAt.After(now.Add(time.Hour)) {
		return fmt.Errorf("updated_at timestamp is too far in the future: %v", doc.UpdatedAt)
	}

	if doc.IndexedAt.After(now.Add(time.Hour)) {
		return fmt.Errorf("indexed_at timestamp is too far in the future: %v", doc.IndexedAt)
	}

	// CreatedAt should not be after UpdatedAt
	if doc.CreatedAt.After(doc.UpdatedAt) {
		return fmt.Errorf("created_at (%v) cannot be after updated_at (%v)", doc.CreatedAt, doc.UpdatedAt)
	}

	return nil
}

// ToMap converts the document to a map for OpenSearch indexing
func (doc *OpenSearchDocument) ToMap() map[string]interface{} {
	// Ensure embedding is never nil
	embedding := doc.Embedding
	if embedding == nil {
		log.Printf("WARNING: embedding is nil for document %s, using empty slice", doc.ID)
		embedding = []float64{}
	}

	result := map[string]interface{}{
		"title":      doc.Title,
		"content":    doc.Content,
		"body":       doc.Body,
		"category":   doc.Category,
		"tags":       doc.Tags,
		"author":     doc.Author,
		"reference":  doc.Reference,
		"source":     doc.Source,
		"file_path":  doc.FilePath,
		"word_count": doc.WordCount,
		"created_at": doc.CreatedAt.Format(time.RFC3339),
		"updated_at": doc.UpdatedAt.Format(time.RFC3339),
		"indexed_at": doc.IndexedAt.Format(time.RFC3339),
		"embedding":  embedding,
	}

	// Add Japanese processed content if present
	if doc.ContentJa != "" {
		result["content_ja"] = doc.ContentJa
	}

	// Add chunk information if present
	if doc.ChunkIndex != nil {
		result["chunk_index"] = *doc.ChunkIndex
	}
	if doc.TotalChunks != nil {
		result["total_chunks"] = *doc.TotalChunks
	}

	// Add custom fields if present
	if len(doc.CustomFields) > 0 {
		result["custom_fields"] = doc.CustomFields
	}

	return result
}

// MarshalJSON implements custom JSON marshaling for OpenSearchDocument
func (doc *OpenSearchDocument) MarshalJSON() ([]byte, error) {
	// Create a custom struct that explicitly includes all fields
	type JSONDoc struct {
		ID           string                 `json:"id"`
		Title        string                 `json:"title"`
		Content      string                 `json:"content"`
		ContentJa    string                 `json:"content_ja"`
		Body         string                 `json:"body"`
		Category     string                 `json:"category"`
		Tags         []string               `json:"tags"`
		Author       string                 `json:"author"`
		Reference    string                 `json:"reference"`
		Source       string                 `json:"source"`
		FilePath     string                 `json:"file_path"`
		WordCount    int                    `json:"word_count"`
		CreatedAt    string                 `json:"created_at"`
		UpdatedAt    string                 `json:"updated_at"`
		IndexedAt    string                 `json:"indexed_at"`
		Embedding    []float64              `json:"embedding"`
		ChunkIndex   *int                   `json:"chunk_index,omitempty"`
		TotalChunks  *int                   `json:"total_chunks,omitempty"`
		CustomFields map[string]interface{} `json:"custom_fields,omitempty"`
	}

	jsonDoc := &JSONDoc{
		ID:           doc.ID,
		Title:        doc.Title,
		Content:      doc.Content,
		ContentJa:    doc.ContentJa,
		Body:         doc.Body,
		Category:     doc.Category,
		Tags:         doc.Tags,
		Author:       doc.Author,
		Reference:    doc.Reference,
		Source:       doc.Source,
		FilePath:     doc.FilePath,
		WordCount:    doc.WordCount,
		CreatedAt:    doc.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    doc.UpdatedAt.Format(time.RFC3339),
		IndexedAt:    doc.IndexedAt.Format(time.RFC3339),
		Embedding:    doc.Embedding,
		ChunkIndex:   doc.ChunkIndex,
		TotalChunks:  doc.TotalChunks,
		CustomFields: doc.CustomFields,
	}

	return json.Marshal(jsonDoc)
}

// GetSize returns the approximate size of the document in bytes
func (doc *OpenSearchDocument) GetSize() int {
	size := len(doc.ID) +
		len(doc.Title) +
		len(doc.Content) +
		len(doc.ContentJa) +
		len(doc.Body) +
		len(doc.Category) +
		len(doc.Author) +
		len(doc.Reference) +
		len(doc.Source) +
		len(doc.FilePath)

	// Add tags size
	for _, tag := range doc.Tags {
		size += len(tag)
	}

	// Add embedding size (8 bytes per float64)
	size += len(doc.Embedding) * 8

	// Estimate for timestamps and other fields
	size += 200

	return size
}

// IsEmpty checks if the document is effectively empty
func (doc *OpenSearchDocument) IsEmpty() bool {
	return doc == nil ||
		(doc.ID == "" && doc.Title == "" && doc.Content == "" && len(doc.Embedding) == 0)
}

// Clone creates a deep copy of the document
func (doc *OpenSearchDocument) Clone() *OpenSearchDocument {
	if doc == nil {
		return nil
	}

	clone := &OpenSearchDocument{
		ID:        doc.ID,
		Title:     doc.Title,
		Content:   doc.Content,
		ContentJa: doc.ContentJa,
		Body:      doc.Body,
		Category:  doc.Category,
		Author:    doc.Author,
		Reference: doc.Reference,
		Source:    doc.Source,
		FilePath:  doc.FilePath,
		WordCount: doc.WordCount,
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
		IndexedAt: doc.IndexedAt,
	}

	// Deep copy chunk information
	if doc.ChunkIndex != nil {
		chunkIndex := *doc.ChunkIndex
		clone.ChunkIndex = &chunkIndex
	}
	if doc.TotalChunks != nil {
		totalChunks := *doc.TotalChunks
		clone.TotalChunks = &totalChunks
	}

	// Deep copy tags
	if len(doc.Tags) > 0 {
		clone.Tags = make([]string, len(doc.Tags))
		copy(clone.Tags, doc.Tags)
	}

	// Deep copy embedding
	if len(doc.Embedding) > 0 {
		clone.Embedding = make([]float64, len(doc.Embedding))
		copy(clone.Embedding, doc.Embedding)
	}

	// Deep copy custom fields
	if len(doc.CustomFields) > 0 {
		clone.CustomFields = make(map[string]interface{})
		for k, v := range doc.CustomFields {
			clone.CustomFields[k] = v
		}
	}

	return clone
}
