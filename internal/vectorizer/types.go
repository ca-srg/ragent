package vectorizer

import (
	"github.com/ca-srg/kiberag/internal/types"
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
	ErrorTypeRateLimit      = types.ErrorTypeRateLimit
	ErrorTypeValidation     = types.ErrorTypeValidation
	ErrorTypeUnknown        = types.ErrorTypeUnknown
)
