package vectorizer

import (
	"github.com/ca-srg/ragent/internal/ingestion/domain"
	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

// Type aliases for common types
type (
	VectorData       = domain.VectorData
	ProcessingResult = domain.ProcessingResult
	Config           = pkgconfig.Config
	DocumentMetadata = domain.DocumentMetadata
	FileInfo         = domain.FileInfo
	ProcessingError  = domain.ProcessingError
	ErrorType        = pkgconfig.ErrorType
)

// Re-export constants
const (
	ErrorTypeFileRead       = pkgconfig.ErrorTypeFileRead
	ErrorTypeMetadata       = pkgconfig.ErrorTypeMetadata
	ErrorTypeEmbedding      = pkgconfig.ErrorTypeEmbedding
	ErrorTypeS3Upload       = pkgconfig.ErrorTypeS3Upload
	ErrorTypeNetworkTimeout = pkgconfig.ErrorTypeNetworkTimeout
	ErrorTypeTimeout        = pkgconfig.ErrorTypeTimeout
	ErrorTypeRateLimit      = pkgconfig.ErrorTypeRateLimit
	ErrorTypeValidation     = pkgconfig.ErrorTypeValidation
	ErrorTypeAuthentication = pkgconfig.ErrorTypeAuthentication
	ErrorTypeUnknown        = pkgconfig.ErrorTypeUnknown
	// OpenSearch specific error types
	ErrorTypeOpenSearchConnection = pkgconfig.ErrorTypeOpenSearchConnection
	ErrorTypeOpenSearchMapping    = pkgconfig.ErrorTypeOpenSearchMapping
	ErrorTypeOpenSearchIndexing   = pkgconfig.ErrorTypeOpenSearchIndexing
	ErrorTypeOpenSearchBulkIndex  = pkgconfig.ErrorTypeOpenSearchBulkIndex
	ErrorTypeOpenSearchQuery      = pkgconfig.ErrorTypeOpenSearchQuery
	ErrorTypeOpenSearchIndex      = pkgconfig.ErrorTypeOpenSearchIndex
)
