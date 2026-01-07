package spreadsheet

import (
	"time"
)

// Config represents the spreadsheet configuration loaded from YAML
type Config struct {
	GCP          GCPConfig     `yaml:"gcp"`
	Spreadsheets []SheetConfig `yaml:"spreadsheets"`
}

// GCPConfig contains GCP authentication settings
type GCPConfig struct {
	// CredentialsFile path to Workload Identity Federation config JSON
	// If empty, falls back to GOOGLE_APPLICATION_CREDENTIALS env var
	CredentialsFile string `yaml:"credentials_file"`
}

// SheetConfig defines a single spreadsheet to process
type SheetConfig struct {
	// ID is the Google Spreadsheet ID (required)
	// Example: "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms"
	ID string `yaml:"id"`

	// Sheet is the sheet name to process (optional, defaults to first sheet)
	Sheet string `yaml:"sheet"`

	// Content defines how to extract content for vectorization
	Content ContentConfig `yaml:"content"`

	// Metadata defines column mappings for document metadata
	Metadata MetadataMapping `yaml:"metadata"`
}

// ContentConfig defines content extraction settings
type ContentConfig struct {
	// Columns specifies which columns to use for content (optional)
	// If empty and AutoDetect is true, columns will be auto-detected
	Columns []string `yaml:"columns"`

	// Template for combining multiple columns (optional)
	// Uses {{column_name}} placeholders
	// Example: "{{タイトル}}\n\n{{詳細}}"
	Template string `yaml:"template"`

	// AutoDetect enables automatic content column detection (default: true)
	AutoDetect *bool `yaml:"auto_detect"`
}

// IsAutoDetectEnabled returns whether auto-detection is enabled
func (c *ContentConfig) IsAutoDetectEnabled() bool {
	if c.AutoDetect == nil {
		return true // Default to true
	}
	return *c.AutoDetect
}

// MetadataMapping defines column to metadata field mappings
type MetadataMapping struct {
	// Title column name for document title
	Title string `yaml:"title"`

	// Category column name for document category
	Category string `yaml:"category"`

	// Tags column names to be used as tags
	Tags []string `yaml:"tags"`

	// ID column name for document identifier
	ID string `yaml:"id"`

	// CreatedAt column name for creation date
	CreatedAt string `yaml:"created_at"`

	// UpdatedAt column name for update date
	UpdatedAt string `yaml:"updated_at"`

	// Reference column name for reference information
	Reference string `yaml:"reference"`
}

// RowData represents a single row from a spreadsheet
type RowData struct {
	// RowIndex is the 1-based row number (excluding header)
	RowIndex int

	// Values maps column header to cell value
	Values map[string]string

	// SpreadsheetID is the source spreadsheet ID
	SpreadsheetID string

	// SheetName is the source sheet name
	SheetName string
}

// FetchResult contains the result of fetching spreadsheet data
type FetchResult struct {
	// Rows contains all fetched rows
	Rows []*RowData

	// Headers contains the column headers
	Headers []string

	// TotalRows is the total number of rows (including skipped)
	TotalRows int

	// SkippedRows is the number of empty rows skipped
	SkippedRows int

	// DetectedContentColumns contains auto-detected content column names
	DetectedContentColumns []string

	// FetchedAt is the timestamp when data was fetched
	FetchedAt time.Time

	// SpreadsheetID is the source spreadsheet ID
	SpreadsheetID string

	// SheetName is the source sheet name
	SheetName string
}

// DryRunInfo contains information for dry-run preview
type DryRunInfo struct {
	RowIndex       int
	ID             string
	Title          string
	Category       string
	ContentPreview string
	ContentLength  int
	ContentColumns []string
}
