package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

// Reader reads CSV files and converts rows to FileInfo
type Reader struct {
	config *Config
}

// NewReader creates a new CSV Reader
func NewReader(config *Config) *Reader {
	if config == nil {
		config = NewDefaultConfig()
	}
	return &Reader{
		config: config,
	}
}

// ReadFile reads a CSV file and returns FileInfo slice (one per row)
func (r *Reader) ReadFile(filePath string) ([]*types.FileInfo, error) {
	// Get configuration for this file
	fileConfig := r.config.GetConfigForFile(filePath)
	if fileConfig == nil {
		return nil, fmt.Errorf("no configuration found for CSV file: %s (no pattern matches)", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	return r.readWithConfig(file, filePath, fileConfig)
}

// readWithConfig reads CSV data from an io.Reader using the specified FileConfig
func (r *Reader) readWithConfig(reader io.Reader, sourcePath string, fileConfig *FileConfig) ([]*types.FileInfo, error) {
	csvReader := csv.NewReader(reader)

	// Read all records
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty: %s", sourcePath)
	}

	// Get header row position (1-indexed)
	headerRowNum := fileConfig.GetHeaderRow()
	headerIndex := headerRowNum - 1 // Convert to 0-indexed

	if headerIndex >= len(records) {
		return nil, fmt.Errorf("header_row %d exceeds total rows %d in file %s", headerRowNum, len(records), sourcePath)
	}

	// Get headers from the specified row
	headers := records[headerIndex]
	if !isValidHeader(headers) {
		return nil, fmt.Errorf("invalid CSV header row (row %d) in file %s: headers appear to be empty or invalid", headerRowNum, sourcePath)
	}

	// Data rows start after the header row
	dataStartIndex := headerIndex + 1
	if dataStartIndex >= len(records) {
		// Only header row, no data
		return nil, nil
	}

	dataRows := records[dataStartIndex:]

	// Create column detector with sample rows
	detector := NewColumnDetector(headers, dataRows)

	// Determine content columns
	contentColumns := fileConfig.Content.Columns
	if len(contentColumns) == 0 && fileConfig.Content.IsAutoDetectEnabled() {
		contentColumns = detector.DetectContentColumns()
	}

	if len(contentColumns) == 0 {
		return nil, fmt.Errorf("no content columns found in CSV file %s: please specify columns in config or ensure data has detectable content columns", sourcePath)
	}

	// Convert rows to FileInfo
	var files []*types.FileInfo
	for rowIdx, row := range dataRows {
		// Skip empty rows
		if isEmptyRow(row) {
			continue
		}

		// Calculate the actual row number in the CSV file (1-indexed)
		// dataStartIndex is 0-indexed, rowIdx is also 0-indexed within dataRows
		actualRowNum := dataStartIndex + rowIdx + 1 // +1 to convert to 1-indexed
		fileInfo := r.rowToFileInfo(row, headers, sourcePath, actualRowNum, detector, contentColumns, fileConfig)
		if fileInfo != nil {
			files = append(files, fileInfo)
		}
	}

	return files, nil
}

// isValidHeader checks if the header row is valid
func isValidHeader(headers []string) bool {
	if len(headers) == 0 {
		return false
	}

	// At least one non-empty header
	for _, h := range headers {
		if strings.TrimSpace(h) != "" {
			return true
		}
	}
	return false
}

// rowToFileInfo converts a CSV row to FileInfo
func (r *Reader) rowToFileInfo(
	row []string,
	headers []string,
	sourcePath string,
	rowIndex int,
	detector *ColumnDetector,
	contentColumns []string,
	fileConfig *FileConfig,
) *types.FileInfo {
	// Build content
	content := buildContent(row, headers, contentColumns, fileConfig)
	if strings.TrimSpace(content) == "" {
		return nil
	}

	// Extract metadata
	metadata := extractMetadata(row, headers, detector, fileConfig)

	// Generate document ID
	docID := generateDocumentID(sourcePath, rowIndex, row, headers, detector, fileConfig)

	// Set FilePath in metadata for consistency
	metadata.FilePath = fmt.Sprintf("csv://%s/row/%d", sourcePath, rowIndex)
	metadata.Source = "csv"
	metadata.WordCount = len(strings.Fields(content))

	return &types.FileInfo{
		Path:        fmt.Sprintf("csv://%s/%s", sourcePath, docID),
		Name:        docID,
		Size:        int64(len(content)),
		ModTime:     time.Now(),
		IsMarkdown:  false,
		IsCSV:       true,
		CSVRowIndex: rowIndex,
		Content:     content,
		Metadata:    metadata,
	}
}

// buildContent builds the content string from row data
func buildContent(row []string, headers []string, contentColumns []string, fileConfig *FileConfig) string {
	if fileConfig.Content.Template != "" {
		return applyTemplate(row, headers, fileConfig.Content.Template)
	}

	// Combine content columns
	var parts []string
	for _, colName := range contentColumns {
		value := getColumnValue(row, headers, colName)
		if value != "" {
			parts = append(parts, value)
		}
	}

	return strings.Join(parts, "\n\n")
}

// applyTemplate applies a template to row data
func applyTemplate(row []string, headers []string, template string) string {
	result := template

	// Replace {{column_name}} placeholders
	re := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	result = re.ReplaceAllStringFunc(result, func(match string) string {
		colName := strings.Trim(match, "{}")
		return getColumnValue(row, headers, colName)
	})

	return result
}

// extractMetadata extracts metadata from row data
func extractMetadata(row []string, headers []string, detector *ColumnDetector, fileConfig *FileConfig) types.DocumentMetadata {
	metadata := types.DocumentMetadata{
		Source:       "csv",
		CustomFields: make(map[string]interface{}),
	}

	cfg := fileConfig.Metadata

	// Title
	titleCol := cfg.Title
	if titleCol == "" {
		titleCol = detector.DetectTitleColumn()
	}
	if titleCol != "" {
		metadata.Title = getColumnValue(row, headers, titleCol)
	}

	// Category
	categoryCol := cfg.Category
	if categoryCol == "" {
		categoryCol = detector.DetectCategoryColumn()
	}
	if categoryCol != "" {
		metadata.Category = getColumnValue(row, headers, categoryCol)
	}

	// Tags
	if len(cfg.Tags) > 0 {
		var tags []string
		for _, tagCol := range cfg.Tags {
			value := getColumnValue(row, headers, tagCol)
			if value != "" {
				tags = append(tags, value)
			}
		}
		metadata.Tags = tags
	}

	// Reference
	if cfg.Reference != "" {
		metadata.Reference = getColumnValue(row, headers, cfg.Reference)
	}

	// CreatedAt
	if cfg.CreatedAt != "" {
		dateStr := getColumnValue(row, headers, cfg.CreatedAt)
		if dateStr != "" {
			if parsedTime, err := parseDate(dateStr); err == nil {
				metadata.CreatedAt = parsedTime
			}
		}
	}

	// UpdatedAt
	if cfg.UpdatedAt != "" {
		dateStr := getColumnValue(row, headers, cfg.UpdatedAt)
		if dateStr != "" {
			if parsedTime, err := parseDate(dateStr); err == nil {
				metadata.UpdatedAt = parsedTime
			}
		}
	}

	return metadata
}

// generateDocumentID generates a unique document ID
func generateDocumentID(sourcePath string, rowIndex int, row []string, headers []string, detector *ColumnDetector, fileConfig *FileConfig) string {
	cfg := fileConfig.Metadata

	// Use configured ID column
	idCol := cfg.ID
	if idCol == "" {
		idCol = detector.DetectIDColumn()
	}

	if idCol != "" {
		id := getColumnValue(row, headers, idCol)
		if id != "" {
			// Sanitize the ID for use in paths
			sanitizedID := sanitizeID(id)
			return fmt.Sprintf("csv_%s_row%d_%s", sanitizeFilename(sourcePath), rowIndex, sanitizedID)
		}
	}

	// Fall back to row index
	return fmt.Sprintf("csv_%s_row%d", sanitizeFilename(sourcePath), rowIndex)
}

// Helper functions

// isEmptyRow checks if a row is empty (all cells are empty or whitespace)
func isEmptyRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// getColumnValue gets a column value by header name
func getColumnValue(row []string, headers []string, columnName string) string {
	if columnName == "" {
		return ""
	}

	for i, header := range headers {
		if strings.EqualFold(header, columnName) {
			if i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
	}
	return ""
}

// parseDate parses a date string in various formats
func parseDate(dateStr string) (time.Time, error) {
	formats := []string{
		"2006/01/02",
		"2006-01-02",
		"2006/1/2",
		"2006-1-2",
		time.RFC3339,
		"2006/01/02 15:04:05",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// sanitizeFilename creates a safe filename from a path
func sanitizeFilename(path string) string {
	// Extract just the filename
	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]

	// Remove extension
	if idx := strings.LastIndex(filename, "."); idx > 0 {
		filename = filename[:idx]
	}

	// Replace unsafe characters
	filename = strings.ReplaceAll(filename, " ", "_")
	filename = strings.ReplaceAll(filename, ".", "_")

	return filename
}

// sanitizeID creates a safe ID string
func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, " ", "_")
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	return id
}

// GetDetectedColumns returns information about detected columns for a CSV file
// Useful for dry-run and debugging
func (r *Reader) GetDetectedColumns(filePath string) (*DetectedColumnsInfo, error) {
	// Get configuration for this file
	fileConfig := r.config.GetConfigForFile(filePath)
	if fileConfig == nil {
		return nil, fmt.Errorf("no configuration found for CSV file: %s (no pattern matches)", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	// Get header row position (1-indexed)
	headerRowNum := fileConfig.GetHeaderRow()
	headerIndex := headerRowNum - 1 // Convert to 0-indexed

	if headerIndex >= len(records) {
		return nil, fmt.Errorf("header_row %d exceeds total rows %d", headerRowNum, len(records))
	}

	headers := records[headerIndex]
	if !isValidHeader(headers) {
		return nil, fmt.Errorf("invalid CSV header row (row %d)", headerRowNum)
	}

	// Data rows start after the header row
	var dataRows [][]string
	dataStartIndex := headerIndex + 1
	if dataStartIndex < len(records) {
		dataRows = records[dataStartIndex:]
	}

	detector := NewColumnDetector(headers, dataRows)

	info := &DetectedColumnsInfo{
		Headers:        headers,
		TotalRows:      len(dataRows),
		HeaderRow:      headerRowNum,
		ContentColumns: fileConfig.Content.Columns,
		TitleColumn:    fileConfig.Metadata.Title,
		CategoryColumn: fileConfig.Metadata.Category,
		IDColumn:       fileConfig.Metadata.ID,
		IsAutoDetected: false,
		MatchedPattern: fileConfig.Pattern,
	}

	// If using auto-detection
	if len(info.ContentColumns) == 0 && fileConfig.Content.IsAutoDetectEnabled() {
		info.ContentColumns = detector.DetectContentColumns()
		info.IsAutoDetected = true
	}

	if info.TitleColumn == "" {
		info.TitleColumn = detector.DetectTitleColumn()
		if info.TitleColumn != "" {
			info.IsAutoDetected = true
		}
	}

	if info.CategoryColumn == "" {
		info.CategoryColumn = detector.DetectCategoryColumn()
		if info.CategoryColumn != "" {
			info.IsAutoDetected = true
		}
	}

	if info.IDColumn == "" {
		info.IDColumn = detector.DetectIDColumn()
		if info.IDColumn != "" {
			info.IsAutoDetected = true
		}
	}

	return info, nil
}

// DetectedColumnsInfo contains information about detected columns
type DetectedColumnsInfo struct {
	Headers        []string
	TotalRows      int
	HeaderRow      int // 1-indexed row number where headers are located
	ContentColumns []string
	TitleColumn    string
	CategoryColumn string
	IDColumn       string
	IsAutoDetected bool
	MatchedPattern string
}

// GetDetectedColumnsFromContent returns information about detected columns from content string
// This is useful for S3 files where the content is already downloaded
func (r *Reader) GetDetectedColumnsFromContent(filePath string, content string) (*DetectedColumnsInfo, error) {
	// Get configuration for this file
	fileConfig := r.config.GetConfigForFile(filePath)
	if fileConfig == nil {
		return nil, fmt.Errorf("no configuration found for CSV file: %s (no pattern matches)", filePath)
	}

	csvReader := csv.NewReader(strings.NewReader(content))
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV content is empty")
	}

	// Get header row position (1-indexed)
	headerRowNum := fileConfig.GetHeaderRow()
	headerIndex := headerRowNum - 1 // Convert to 0-indexed

	if headerIndex >= len(records) {
		return nil, fmt.Errorf("header_row %d exceeds total rows %d", headerRowNum, len(records))
	}

	headers := records[headerIndex]
	if !isValidHeader(headers) {
		return nil, fmt.Errorf("invalid CSV header row (row %d)", headerRowNum)
	}

	// Data rows start after the header row
	var dataRows [][]string
	dataStartIndex := headerIndex + 1
	if dataStartIndex < len(records) {
		dataRows = records[dataStartIndex:]
	}

	detector := NewColumnDetector(headers, dataRows)

	info := &DetectedColumnsInfo{
		Headers:        headers,
		TotalRows:      len(dataRows),
		HeaderRow:      headerRowNum,
		ContentColumns: fileConfig.Content.Columns,
		TitleColumn:    fileConfig.Metadata.Title,
		CategoryColumn: fileConfig.Metadata.Category,
		IDColumn:       fileConfig.Metadata.ID,
		IsAutoDetected: false,
		MatchedPattern: fileConfig.Pattern,
	}

	// If using auto-detection
	if len(info.ContentColumns) == 0 && fileConfig.Content.IsAutoDetectEnabled() {
		info.ContentColumns = detector.DetectContentColumns()
		info.IsAutoDetected = true
	}

	if info.TitleColumn == "" {
		info.TitleColumn = detector.DetectTitleColumn()
		if info.TitleColumn != "" {
			info.IsAutoDetected = true
		}
	}

	if info.CategoryColumn == "" {
		info.CategoryColumn = detector.DetectCategoryColumn()
		if info.CategoryColumn != "" {
			info.IsAutoDetected = true
		}
	}

	if info.IDColumn == "" {
		info.IDColumn = detector.DetectIDColumn()
		if info.IDColumn != "" {
			info.IsAutoDetected = true
		}
	}

	return info, nil
}
