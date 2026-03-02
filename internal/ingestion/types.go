// Package ingestion provides the vectorize/list/recreate-index vertical slice.
// This file re-exports the domain types so callers can continue to use
// ingestion.FileInfo, ingestion.ProcessingResult, etc. without breaking changes.
package ingestion

import "github.com/ca-srg/ragent/internal/ingestion/domain"

// Type aliases re-exported from the domain package for backward compatibility.
type (
	DocumentMetadata = domain.DocumentMetadata
	FileInfo         = domain.FileInfo
	VectorData       = domain.VectorData
	ProcessingResult = domain.ProcessingResult
	ProcessingError  = domain.ProcessingError
)
