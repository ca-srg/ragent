package csv

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// Config represents the CSV configuration loaded from YAML
type Config struct {
	CSV CSVConfig `yaml:"csv"`
}

// CSVConfig contains CSV processing settings
type CSVConfig struct {
	// Files defines per-file pattern configurations (required)
	Files []FileConfig `yaml:"files"`
}

// FileConfig defines configuration for files matching a pattern
type FileConfig struct {
	// Pattern is a glob pattern to match filenames (e.g., "important*.csv", "*.csv")
	Pattern string `yaml:"pattern"`

	// HeaderRow specifies which row contains the column headers (1-indexed, default: 1)
	// Rows before this are skipped, and data starts from the row after the header
	HeaderRow *int `yaml:"header_row"`

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

// GetHeaderRow returns the header row number (1-indexed)
// Returns 1 if not specified (first row is header)
func (fc *FileConfig) GetHeaderRow() int {
	if fc.HeaderRow == nil {
		return 1 // Default to first row
	}
	return *fc.HeaderRow
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

// LoadConfig loads and validates CSV configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse CSV config YAML: %w", err)
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid CSV config: %w", err)
	}

	// Apply defaults
	applyDefaults(&config)

	return &config, nil
}

// validateConfig validates the configuration structure
func validateConfig(cfg *Config) error {
	if len(cfg.CSV.Files) == 0 {
		return fmt.Errorf("csv.files is required and must contain at least one file configuration")
	}

	for i, fc := range cfg.CSV.Files {
		if fc.Pattern == "" {
			return fmt.Errorf("csv.files[%d].pattern is required", i)
		}
		// Validate pattern is a valid glob
		if _, err := filepath.Match(fc.Pattern, "test.csv"); err != nil {
			return fmt.Errorf("csv.files[%d].pattern '%s' is invalid: %w", i, fc.Pattern, err)
		}
		// Validate header_row if specified
		if fc.HeaderRow != nil && *fc.HeaderRow < 1 {
			return fmt.Errorf("csv.files[%d].header_row must be >= 1 (1-indexed row number)", i)
		}
	}

	return nil
}

// applyDefaults applies default values to the configuration
func applyDefaults(cfg *Config) {
	for i := range cfg.CSV.Files {
		// Default auto_detect to true if not explicitly set
		if cfg.CSV.Files[i].Content.AutoDetect == nil {
			autoDetect := true
			cfg.CSV.Files[i].Content.AutoDetect = &autoDetect
		}
	}
}

// GetConfigForFile returns the FileConfig that matches the given filename
// Returns nil if no pattern matches
func (c *Config) GetConfigForFile(filename string) *FileConfig {
	// Extract just the filename without path
	baseName := filepath.Base(filename)

	// Normalize both pattern and filename to NFC (composed form) for consistent matching
	// This fixes issues where S3 filenames may be in NFD (decomposed) form
	// while YAML config patterns are typically in NFC form
	normalizedBaseName := norm.NFC.String(baseName)

	for i := range c.CSV.Files {
		normalizedPattern := norm.NFC.String(c.CSV.Files[i].Pattern)
		matched, err := filepath.Match(normalizedPattern, normalizedBaseName)
		if err != nil {
			continue // Invalid pattern, skip
		}
		if matched {
			return &c.CSV.Files[i]
		}
	}

	return nil
}

// HasConfigForFile checks if there's a matching configuration for the given filename
func (c *Config) HasConfigForFile(filename string) bool {
	return c.GetConfigForFile(filename) != nil
}

// NewDefaultConfig creates a default configuration with auto-detection enabled
func NewDefaultConfig() *Config {
	autoDetect := true
	return &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern: "*.csv",
					Content: ContentConfig{
						AutoDetect: &autoDetect,
					},
				},
			},
		},
	}
}
