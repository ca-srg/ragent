//go:build tools

// Package pdf handles PDF OCR and page expansion for the ingestion pipeline.
// This file ensures build tool dependencies are tracked in go.mod.
package pdf

import _ "github.com/pdfcpu/pdfcpu/pkg/api"
