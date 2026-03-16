package vectorizer

import (
	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
)

// Type aliases for common types
type (
	VectorData       = pkgdomain.VectorData
	ProcessingResult = pkgdomain.ProcessingResult
	Config           = pkgconfig.Config
	DocumentMetadata = pkgdomain.DocumentMetadata
	FileInfo         = pkgdomain.FileInfo
	ProcessingError  = pkgdomain.ProcessingError
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
