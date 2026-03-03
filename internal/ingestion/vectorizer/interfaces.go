package vectorizer

import (
	"context"
)

// EmbeddingClient defines the interface for generating embeddings from text
type EmbeddingClient interface {
	// GenerateEmbedding creates an embedding vector from the given text
	GenerateEmbedding(ctx context.Context, text string) ([]float64, error)

	// ValidateConnection checks if the embedding service is accessible
	ValidateConnection(ctx context.Context) error

	// GetModelInfo returns information about the embedding model being used
	GetModelInfo() (string, int, error) // model name, dimension, error
}

// S3VectorClient defines the interface for storing vectors in S3
type S3VectorClient interface {
	// StoreVector saves a vector with its metadata to S3
	StoreVector(ctx context.Context, vectorData *VectorData) error

	// ValidateAccess checks if S3 bucket is accessible
	ValidateAccess(ctx context.Context) error

	// ListVectors returns a list of stored vector keys (optional, for debugging)
	ListVectors(ctx context.Context, prefix string) ([]string, error)

	// DeleteVector removes a vector from S3 (optional, for cleanup)
	DeleteVector(ctx context.Context, vectorID string) error

	// GetBucketInfo returns information about the S3 bucket
	GetBucketInfo(ctx context.Context) (map[string]interface{}, error)
}

// MetadataExtractor defines the interface for extracting metadata from files
type MetadataExtractor interface {
	// ExtractMetadata extracts metadata from a file's content and path
	ExtractMetadata(filePath string, content string) (*DocumentMetadata, error)

	// ParseFrontMatter extracts YAML front matter from markdown content
	ParseFrontMatter(content string) (map[string]interface{}, string, error)

	// GenerateKey creates a unique key for the document
	GenerateKey(metadata *DocumentMetadata) string
}

// FileScanner defines the interface for scanning and processing files
type FileScanner interface {
	// ScanDirectory scans a directory for supported files (markdown and CSV)
	ScanDirectory(dirPath string) ([]*FileInfo, error)

	// ValidateDirectory checks if the directory exists and is readable
	ValidateDirectory(dirPath string) error

	// ReadFileContent reads and returns the content of a file
	ReadFileContent(filePath string) (string, error)

	// IsMarkdownFile checks if a file is a markdown file
	IsMarkdownFile(filePath string) bool

	// IsCSVFile checks if a file is a CSV file
	IsCSVFile(filePath string) bool

	// IsSupportedFile checks if a file is a supported file type (markdown or CSV)
	IsSupportedFile(filePath string) bool
}

// ConcurrencyController defines the interface for managing concurrent processing
type ConcurrencyController interface {
	// ProcessConcurrently processes multiple files with controlled concurrency
	ProcessConcurrently(ctx context.Context, files []*FileInfo, processFn func(*FileInfo) error) *ProcessingResult

	// SetConcurrency sets the maximum number of concurrent operations
	SetConcurrency(maxConcurrency int)

	// GetConcurrency returns the current concurrency limit
	GetConcurrency() int
}

// Validator defines the interface for validating configuration and connections
type Validator interface {
	// ValidateConfig checks if all required configuration is present and valid
	ValidateConfig(config *Config) error

	// ValidateConnections tests connections to external services
	ValidateConnections(ctx context.Context, config *Config) error

	// GenerateConfigGuide returns a guide for setting up configuration
	GenerateConfigGuide() string
}

// OpenSearchIndexer defines the interface for indexing documents in OpenSearch
type OpenSearchIndexer interface {
	// IndexDocument indexes a single document in OpenSearch
	IndexDocument(ctx context.Context, indexName string, document *OpenSearchDocument) error

	// IndexDocuments indexes multiple documents in OpenSearch using bulk operations
	IndexDocuments(ctx context.Context, indexName string, documents []*OpenSearchDocument) error

	// ValidateConnection checks if OpenSearch is accessible and responsive
	ValidateConnection(ctx context.Context) error

	// CreateIndex creates a new index with appropriate mappings for vector search
	CreateIndex(ctx context.Context, indexName string, dimension int) error

	// DeleteIndex removes an existing index (use with caution)
	DeleteIndex(ctx context.Context, indexName string) error

	// IndexExists checks if an index exists in OpenSearch
	IndexExists(ctx context.Context, indexName string) (bool, error)

	// GetIndexInfo returns information about an index (mappings, settings, document count)
	GetIndexInfo(ctx context.Context, indexName string) (map[string]interface{}, error)

	// RefreshIndex forces a refresh of the index to make recent changes visible
	RefreshIndex(ctx context.Context, indexName string) error

	// GetDocumentCount returns the number of documents in an index
	GetDocumentCount(ctx context.Context, indexName string) (int64, error)

	// ProcessJapaneseText processes text using Japanese analyzer for better indexing
	ProcessJapaneseText(text string) (string, error)
}
