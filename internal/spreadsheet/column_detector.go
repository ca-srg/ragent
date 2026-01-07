package spreadsheet

import (
	"strings"
	"unicode/utf8"
)

// Known content column names (case-insensitive matching)
var knownContentColumnNames = []string{
	// Japanese
	"詳細", "本文", "内容", "説明", "備考", "コメント", "メモ",
	// English
	"content", "description", "body", "text", "details", "notes", "comment", "memo",
}

// Known title column names (case-insensitive matching)
var knownTitleColumnNames = []string{
	// Japanese
	"タイトル", "件名", "題名", "名前", "名称",
	// English
	"title", "subject", "name", "heading",
}

// Known category column names (case-insensitive matching)
var knownCategoryColumnNames = []string{
	// Japanese
	"カテゴリ", "カテゴリー", "分類", "種別", "種類",
	// English
	"category", "type", "classification", "kind",
}

// Known ID column names (case-insensitive matching)
var knownIDColumnNames = []string{
	"id", "ID", "Id", "#", "番号", "No", "NO", "no", "num", "number",
}

// ColumnDetector detects appropriate columns for content and metadata
type ColumnDetector struct {
	headers    []string
	sampleRows [][]string
}

// NewColumnDetector creates a new ColumnDetector
func NewColumnDetector(headers []string, sampleRows [][]string) *ColumnDetector {
	return &ColumnDetector{
		headers:    headers,
		sampleRows: sampleRows,
	}
}

// DetectContentColumns finds the best column(s) for vectorization content
// Returns column names that should be used for content
func (d *ColumnDetector) DetectContentColumns() []string {
	// 1. Try to find columns with known content names
	for _, header := range d.headers {
		if matchesAny(header, knownContentColumnNames) {
			return []string{header}
		}
	}

	// 2. Fall back to finding the column with the highest average character count
	return d.detectByAverageLength()
}

// DetectTitleColumn finds the title column
func (d *ColumnDetector) DetectTitleColumn() string {
	for _, header := range d.headers {
		if matchesAny(header, knownTitleColumnNames) {
			return header
		}
	}
	return ""
}

// DetectCategoryColumn finds the category column
func (d *ColumnDetector) DetectCategoryColumn() string {
	for _, header := range d.headers {
		if matchesAny(header, knownCategoryColumnNames) {
			return header
		}
	}
	return ""
}

// DetectIDColumn finds the ID column
func (d *ColumnDetector) DetectIDColumn() string {
	for _, header := range d.headers {
		if matchesAny(header, knownIDColumnNames) {
			return header
		}
	}
	return ""
}

// detectByAverageLength finds columns with the highest average text length
func (d *ColumnDetector) detectByAverageLength() []string {
	if len(d.sampleRows) == 0 || len(d.headers) == 0 {
		return nil
	}

	// Calculate average length for each column
	type columnStats struct {
		header        string
		avgLength     float64
		nonEmptyCount int
	}

	stats := make([]columnStats, len(d.headers))

	for colIdx, header := range d.headers {
		var totalLength int
		var nonEmptyCount int

		for _, row := range d.sampleRows {
			if colIdx < len(row) {
				cellValue := strings.TrimSpace(row[colIdx])
				if cellValue != "" {
					totalLength += utf8.RuneCountInString(cellValue)
					nonEmptyCount++
				}
			}
		}

		var avgLength float64
		if nonEmptyCount > 0 {
			avgLength = float64(totalLength) / float64(nonEmptyCount)
		}

		stats[colIdx] = columnStats{
			header:        header,
			avgLength:     avgLength,
			nonEmptyCount: nonEmptyCount,
		}
	}

	// Find the column with the highest average length
	// Minimum threshold: average length > 50 characters
	const minAvgLength = 50.0
	var bestColumn string
	var bestAvgLength float64

	for _, s := range stats {
		if s.avgLength > bestAvgLength && s.avgLength >= minAvgLength {
			bestAvgLength = s.avgLength
			bestColumn = s.header
		}
	}

	if bestColumn != "" {
		return []string{bestColumn}
	}

	// If no column meets the threshold, return the one with highest avg length
	for _, s := range stats {
		if s.avgLength > bestAvgLength {
			bestAvgLength = s.avgLength
			bestColumn = s.header
		}
	}

	if bestColumn != "" {
		return []string{bestColumn}
	}

	return nil
}

// matchesAny checks if the header matches any of the known names (case-insensitive)
func matchesAny(header string, knownNames []string) bool {
	headerLower := strings.ToLower(strings.TrimSpace(header))
	for _, name := range knownNames {
		if headerLower == strings.ToLower(name) {
			return true
		}
	}
	return false
}

// GetColumnIndex returns the index of a column by header name
// Returns -1 if not found
func (d *ColumnDetector) GetColumnIndex(columnName string) int {
	for i, header := range d.headers {
		if strings.EqualFold(header, columnName) {
			return i
		}
	}
	return -1
}

// ColumnExists checks if a column exists in the headers
func (d *ColumnDetector) ColumnExists(columnName string) bool {
	return d.GetColumnIndex(columnName) >= 0
}
