package vectorizer

import (
	"github.com/ca-srg/mdrag/internal/types"
)

// Type aliases for common types
type (
	VectorData       = types.VectorData
	ProcessingResult = types.ProcessingResult
	Config           = types.Config
	DocumentMetadata = types.DocumentMetadata
	FileInfo         = types.FileInfo
	ProcessingError  = types.ProcessingError
	ErrorType        = types.ErrorType
)

// Re-export constants
const (
	ErrorTypeFileRead       = types.ErrorTypeFileRead
	ErrorTypeMetadata       = types.ErrorTypeMetadata
	ErrorTypeEmbedding      = types.ErrorTypeEmbedding
	ErrorTypeS3Upload       = types.ErrorTypeS3Upload
	ErrorTypeNetworkTimeout = types.ErrorTypeNetworkTimeout
	ErrorTypeTimeout        = types.ErrorTypeTimeout
	ErrorTypeRateLimit      = types.ErrorTypeRateLimit
	ErrorTypeValidation     = types.ErrorTypeValidation
	ErrorTypeAuthentication = types.ErrorTypeAuthentication
	ErrorTypeUnknown        = types.ErrorTypeUnknown
	// OpenSearch specific error types
	ErrorTypeOpenSearchConnection = types.ErrorTypeOpenSearchConnection
	ErrorTypeOpenSearchMapping    = types.ErrorTypeOpenSearchMapping
	ErrorTypeOpenSearchIndexing   = types.ErrorTypeOpenSearchIndexing
	ErrorTypeOpenSearchBulkIndex  = types.ErrorTypeOpenSearchBulkIndex
	ErrorTypeOpenSearchQuery      = types.ErrorTypeOpenSearchQuery
	ErrorTypeOpenSearchIndex      = types.ErrorTypeOpenSearchIndex
)
