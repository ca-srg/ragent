// Package ingestion provides the vectorize/list/recreate-index vertical slice.
// This file re-exports the domain types so callers can continue to use
// ingestion.FileInfo, ingestion.ProcessingResult, etc. without breaking changes.
package ingestion

import pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"

// Type aliases re-exported from the domain package for backward compatibility.
type (
	DocumentMetadata = pkgdomain.DocumentMetadata
	FileInfo         = pkgdomain.FileInfo
	VectorData       = pkgdomain.VectorData
	ProcessingResult = pkgdomain.ProcessingResult
	ProcessingError  = pkgdomain.ProcessingError
)
