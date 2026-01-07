package spreadsheet

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

// Fetcher fetches data from Google Spreadsheets and converts to FileInfo
type Fetcher struct {
	service *SheetsService
	config  *Config
}

// NewFetcher creates a new Fetcher
func NewFetcher(ctx context.Context, config *Config) (*Fetcher, error) {
	credentialsFile := config.GetCredentialsFile()

	service, err := NewSheetsService(ctx, credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	return &Fetcher{
		service: service,
		config:  config,
	}, nil
}

// Fetch retrieves all rows from configured spreadsheets and converts to FileInfo slice
func (f *Fetcher) Fetch(ctx context.Context) ([]*types.FileInfo, error) {
	var allFiles []*types.FileInfo

	for _, sheetCfg := range f.config.Spreadsheets {
		log.Printf("Fetching spreadsheet: %s (%s)", sheetCfg.ID, sheetCfg.Sheet)

		files, err := f.fetchSheet(ctx, sheetCfg)
		if err != nil {
			// Stop on any error as per requirements
			return nil, fmt.Errorf("failed to fetch sheet %s/%s: %w", sheetCfg.ID, sheetCfg.Sheet, err)
		}

		allFiles = append(allFiles, files...)
	}

	return allFiles, nil
}

// FetchWithDryRun retrieves data and returns dry-run information
func (f *Fetcher) FetchWithDryRun(ctx context.Context) ([]*DryRunInfo, error) {
	var allDryRunInfo []*DryRunInfo

	for _, sheetCfg := range f.config.Spreadsheets {
		log.Printf("Fetching spreadsheet: %s (%s)", sheetCfg.ID, sheetCfg.Sheet)

		info, err := f.fetchSheetDryRun(ctx, sheetCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch sheet %s/%s: %w", sheetCfg.ID, sheetCfg.Sheet, err)
		}

		allDryRunInfo = append(allDryRunInfo, info...)
	}

	return allDryRunInfo, nil
}

// fetchSheet fetches a single spreadsheet and converts to FileInfo slice
func (f *Fetcher) fetchSheet(ctx context.Context, cfg SheetConfig) ([]*types.FileInfo, error) {
	// Get sheet data
	valueRange, err := f.service.GetSheetData(ctx, cfg.ID, cfg.Sheet)
	if err != nil {
		return nil, err
	}

	if len(valueRange.Values) < 2 {
		log.Printf("No data rows found in sheet %s/%s", cfg.ID, cfg.Sheet)
		return nil, nil
	}

	// First row is headers
	headers := toStringSlice(valueRange.Values[0])
	dataRows := valueRange.Values[1:]

	// Create column detector
	sampleRows := make([][]string, 0, len(dataRows))
	for _, row := range dataRows {
		sampleRows = append(sampleRows, toStringSlice(row))
	}
	detector := NewColumnDetector(headers, sampleRows)

	// Determine content columns
	contentColumns := cfg.Content.Columns
	if len(contentColumns) == 0 && cfg.Content.IsAutoDetectEnabled() {
		contentColumns = detector.DetectContentColumns()
		if len(contentColumns) > 0 {
			log.Printf("Auto-detected content column(s): %v", contentColumns)
		}
	}

	if len(contentColumns) == 0 {
		return nil, fmt.Errorf("no content columns found for sheet %s/%s", cfg.ID, cfg.Sheet)
	}

	// Convert rows to FileInfo
	var files []*types.FileInfo
	skippedRows := 0

	for rowIdx, row := range dataRows {
		rowData := toStringSlice(row)

		// Skip empty rows
		if isEmptyRow(rowData) {
			skippedRows++
			continue
		}

		fileInfo := f.rowToFileInfo(rowData, headers, cfg, rowIdx+2, detector, contentColumns) // +2 because row 1 is header, rows are 1-indexed
		if fileInfo != nil {
			files = append(files, fileInfo)
		}
	}

	log.Printf("Fetched %d rows from %s/%s (skipped %d empty rows)", len(files), cfg.ID, cfg.Sheet, skippedRows)

	return files, nil
}

// fetchSheetDryRun fetches a single spreadsheet and returns dry-run information
func (f *Fetcher) fetchSheetDryRun(ctx context.Context, cfg SheetConfig) ([]*DryRunInfo, error) {
	// Get sheet data
	valueRange, err := f.service.GetSheetData(ctx, cfg.ID, cfg.Sheet)
	if err != nil {
		return nil, err
	}

	if len(valueRange.Values) < 2 {
		log.Printf("No data rows found in sheet %s/%s", cfg.ID, cfg.Sheet)
		return nil, nil
	}

	// First row is headers
	headers := toStringSlice(valueRange.Values[0])
	dataRows := valueRange.Values[1:]

	// Create column detector
	sampleRows := make([][]string, 0, len(dataRows))
	for _, row := range dataRows {
		sampleRows = append(sampleRows, toStringSlice(row))
	}
	detector := NewColumnDetector(headers, sampleRows)

	// Determine content columns
	contentColumns := cfg.Content.Columns
	if len(contentColumns) == 0 && cfg.Content.IsAutoDetectEnabled() {
		contentColumns = detector.DetectContentColumns()
	}

	// Convert rows to DryRunInfo
	var infos []*DryRunInfo
	skippedRows := 0

	for rowIdx, row := range dataRows {
		rowData := toStringSlice(row)

		// Skip empty rows
		if isEmptyRow(rowData) {
			skippedRows++
			continue
		}

		info := f.rowToDryRunInfo(rowData, headers, cfg, rowIdx+2, detector, contentColumns)
		if info != nil {
			infos = append(infos, info)
		}
	}

	log.Printf("Found %d rows in %s/%s (skipped %d empty rows)", len(infos), cfg.ID, cfg.Sheet, skippedRows)

	return infos, nil
}

// rowToFileInfo converts a spreadsheet row to FileInfo
func (f *Fetcher) rowToFileInfo(
	row []string,
	headers []string,
	cfg SheetConfig,
	rowIndex int,
	detector *ColumnDetector,
	contentColumns []string,
) *types.FileInfo {
	// Build content
	content := f.buildContent(row, headers, cfg, contentColumns)
	if strings.TrimSpace(content) == "" {
		return nil
	}

	// Extract metadata
	metadata := f.extractMetadata(row, headers, cfg, detector)

	// Generate document ID
	docID := f.generateDocumentID(cfg, rowIndex, row, headers, detector)

	// Set FilePath in metadata for consistency with markdown files
	metadata.FilePath = fmt.Sprintf("spreadsheet://%s/%s/row/%d", cfg.ID, cfg.Sheet, rowIndex)

	return &types.FileInfo{
		Path:       fmt.Sprintf("spreadsheet://%s/%s/%s", cfg.ID, cfg.Sheet, docID),
		Name:       docID,
		Size:       int64(len(content)),
		ModTime:    time.Now(),
		IsMarkdown: false,
		Content:    content,
		Metadata:   metadata,
	}
}

// rowToDryRunInfo converts a spreadsheet row to DryRunInfo
func (f *Fetcher) rowToDryRunInfo(
	row []string,
	headers []string,
	cfg SheetConfig,
	rowIndex int,
	detector *ColumnDetector,
	contentColumns []string,
) *DryRunInfo {
	content := f.buildContent(row, headers, cfg, contentColumns)
	if strings.TrimSpace(content) == "" {
		return nil
	}

	// Get ID
	idCol := cfg.Metadata.ID
	if idCol == "" {
		idCol = detector.DetectIDColumn()
	}
	id := getColumnValue(row, headers, idCol)
	if id == "" {
		id = fmt.Sprintf("row_%d", rowIndex)
	}

	// Get title
	titleCol := cfg.Metadata.Title
	if titleCol == "" {
		titleCol = detector.DetectTitleColumn()
	}
	title := getColumnValue(row, headers, titleCol)
	if title == "" {
		title = fmt.Sprintf("Row %d", rowIndex)
	}

	// Get category
	categoryCol := cfg.Metadata.Category
	if categoryCol == "" {
		categoryCol = detector.DetectCategoryColumn()
	}
	category := getColumnValue(row, headers, categoryCol)

	// Create preview
	preview := content
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}

	return &DryRunInfo{
		RowIndex:       rowIndex,
		ID:             id,
		Title:          title,
		Category:       category,
		ContentPreview: preview,
		ContentLength:  len(content),
		ContentColumns: contentColumns,
	}
}

// buildContent builds the content string from row data
func (f *Fetcher) buildContent(row []string, headers []string, cfg SheetConfig, contentColumns []string) string {
	if cfg.Content.Template != "" {
		return f.applyTemplate(row, headers, cfg.Content.Template)
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
func (f *Fetcher) applyTemplate(row []string, headers []string, template string) string {
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
func (f *Fetcher) extractMetadata(row []string, headers []string, cfg SheetConfig, detector *ColumnDetector) types.DocumentMetadata {
	metadata := types.DocumentMetadata{
		Source:       "spreadsheet",
		CustomFields: make(map[string]interface{}),
	}

	// Title
	titleCol := cfg.Metadata.Title
	if titleCol == "" {
		titleCol = detector.DetectTitleColumn()
	}
	if titleCol != "" {
		metadata.Title = getColumnValue(row, headers, titleCol)
	}

	// Category
	categoryCol := cfg.Metadata.Category
	if categoryCol == "" {
		categoryCol = detector.DetectCategoryColumn()
	}
	if categoryCol != "" {
		metadata.Category = getColumnValue(row, headers, categoryCol)
	}

	// Tags
	if len(cfg.Metadata.Tags) > 0 {
		var tags []string
		for _, tagCol := range cfg.Metadata.Tags {
			value := getColumnValue(row, headers, tagCol)
			if value != "" {
				tags = append(tags, value)
			}
		}
		metadata.Tags = tags
	}

	// Reference
	if cfg.Metadata.Reference != "" {
		metadata.Reference = getColumnValue(row, headers, cfg.Metadata.Reference)
	}

	// CreatedAt
	if cfg.Metadata.CreatedAt != "" {
		dateStr := getColumnValue(row, headers, cfg.Metadata.CreatedAt)
		if dateStr != "" {
			if parsedTime, err := parseDate(dateStr); err == nil {
				metadata.CreatedAt = parsedTime
			}
		}
	}

	// UpdatedAt
	if cfg.Metadata.UpdatedAt != "" {
		dateStr := getColumnValue(row, headers, cfg.Metadata.UpdatedAt)
		if dateStr != "" {
			if parsedTime, err := parseDate(dateStr); err == nil {
				metadata.UpdatedAt = parsedTime
			}
		}
	}

	return metadata
}

// generateDocumentID generates a unique document ID
func (f *Fetcher) generateDocumentID(cfg SheetConfig, rowIndex int, row []string, headers []string, detector *ColumnDetector) string {
	// Use configured ID column
	idCol := cfg.Metadata.ID
	if idCol == "" {
		idCol = detector.DetectIDColumn()
	}

	if idCol != "" {
		id := getColumnValue(row, headers, idCol)
		if id != "" {
			return fmt.Sprintf("%s_%s_%s", cfg.ID, cfg.Sheet, id)
		}
	}

	// Fall back to row index
	return fmt.Sprintf("%s_%s_row%d", cfg.ID, cfg.Sheet, rowIndex)
}

// ValidateConnection validates connections to all configured spreadsheets
func (f *Fetcher) ValidateConnection(ctx context.Context) error {
	for _, sheetCfg := range f.config.Spreadsheets {
		if err := f.service.ValidateConnection(ctx, sheetCfg.ID); err != nil {
			return fmt.Errorf("failed to validate spreadsheet %s: %w", sheetCfg.ID, err)
		}
	}
	return nil
}

// Helper functions

// toStringSlice converts []interface{} to []string
func toStringSlice(row []interface{}) []string {
	result := make([]string, len(row))
	for i, v := range row {
		if v != nil {
			result[i] = fmt.Sprintf("%v", v)
		}
	}
	return result
}

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
