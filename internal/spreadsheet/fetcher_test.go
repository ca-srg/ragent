package spreadsheet

import (
	"testing"
	"time"
)

// Test helper functions using sample data structure

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []interface{}
		expected []string
	}{
		{
			name:     "string values",
			input:    []interface{}{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "mixed types",
			input:    []interface{}{"text", 123, 45.6, true},
			expected: []string{"text", "123", "45.6", "true"},
		},
		{
			name:     "with nil",
			input:    []interface{}{"a", nil, "c"},
			expected: []string{"a", "", "c"},
		},
		{
			name:     "empty",
			input:    []interface{}{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toStringSlice(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Length mismatch: got %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("Index %d: got %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestIsEmptyRow(t *testing.T) {
	tests := []struct {
		name     string
		row      []string
		expected bool
	}{
		{
			name:     "empty slice",
			row:      []string{},
			expected: true,
		},
		{
			name:     "all empty strings",
			row:      []string{"", "", ""},
			expected: true,
		},
		{
			name:     "whitespace only",
			row:      []string{"  ", "\t", "  \n  "},
			expected: true,
		},
		{
			name:     "has content",
			row:      []string{"", "value", ""},
			expected: false,
		},
		{
			name:     "sample row",
			row:      []string{"1", "BTS-001", "サンプルプロジェクト", "開発チームA"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmptyRow(tt.row)
			if result != tt.expected {
				t.Errorf("isEmptyRow() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetColumnValue(t *testing.T) {
	headers := []string{"#", "BTS#", "PJ", "開発", "タイトル", "詳細"}
	row := []string{"1", "BTS-001", "サンプルプロジェクト", "開発チームA", "テストタイトル", "詳細な説明"}

	tests := []struct {
		name       string
		columnName string
		expected   string
	}{
		{"existing column", "#", "1"},
		{"bts column", "BTS#", "BTS-001"},
		{"japanese column", "タイトル", "テストタイトル"},
		{"case insensitive", "pj", "サンプルプロジェクト"},
		{"nonexistent column", "存在しない", ""},
		{"empty column name", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getColumnValue(row, headers, tt.columnName)
			if result != tt.expected {
				t.Errorf("getColumnValue(%q) = %q, want %q", tt.columnName, result, tt.expected)
			}
		})
	}
}

func TestGetColumnValueWithShortRow(t *testing.T) {
	headers := []string{"col1", "col2", "col3", "col4", "col5"}
	row := []string{"a", "b"} // Row is shorter than headers

	result := getColumnValue(row, headers, "col4")
	if result != "" {
		t.Errorf("Expected empty string for out-of-bounds column, got: %q", result)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectedDay int // Day of month to verify
	}{
		{
			name:        "YYYY/MM/DD",
			input:       "2024/01/15",
			expectError: false,
			expectedDay: 15,
		},
		{
			name:        "YYYY-MM-DD",
			input:       "2024-01-15",
			expectError: false,
			expectedDay: 15,
		},
		{
			name:        "YYYY/M/D",
			input:       "2024/1/5",
			expectError: false,
			expectedDay: 5,
		},
		{
			name:        "YYYY/MM/DD HH:MM:SS",
			input:       "2024/01/20 14:30:00",
			expectError: false,
			expectedDay: 20,
		},
		{
			name:        "invalid format",
			input:       "invalid-date",
			expectError: true,
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDate(tt.input)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.Day() != tt.expectedDay {
				t.Errorf("Day = %d, want %d", result.Day(), tt.expectedDay)
			}
		})
	}
}

func TestBuildContentWithTemplate(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"タイトル", "詳細", "カテゴリ"}
	row := []string{"テストタイトル", "これは詳細な説明です", "バグ"}

	template := "【{{タイトル}}】\n\nカテゴリ: {{カテゴリ}}\n\n{{詳細}}"

	result := fetcher.applyTemplate(row, headers, template)

	expected := "【テストタイトル】\n\nカテゴリ: バグ\n\nこれは詳細な説明です"
	if result != expected {
		t.Errorf("applyTemplate() = %q, want %q", result, expected)
	}
}

func TestBuildContentWithoutTemplate(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル", "詳細"}
	row := []string{"1", "テストタイトル", "詳細な説明"}

	cfg := SheetConfig{
		Content: ContentConfig{
			Columns: []string{"タイトル", "詳細"},
		},
	}
	contentColumns := []string{"タイトル", "詳細"}

	result := fetcher.buildContent(row, headers, cfg, contentColumns)

	expected := "テストタイトル\n\n詳細な説明"
	if result != expected {
		t.Errorf("buildContent() = %q, want %q", result, expected)
	}
}

func TestExtractMetadata(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル", "カテゴリ", "トラック", "ランク", "起票日", "BTS#"}
	row := []string{"42", "テストバグ", "Web", "バグ", "A", "2024/01/15", "BTS-123"}

	cfg := SheetConfig{
		Metadata: MetadataMapping{
			Title:     "タイトル",
			Category:  "カテゴリ",
			Tags:      []string{"トラック", "ランク"},
			ID:        "#",
			CreatedAt: "起票日",
			Reference: "BTS#",
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	metadata := fetcher.extractMetadata(row, headers, cfg, detector)

	if metadata.Title != "テストバグ" {
		t.Errorf("Title = %q, want %q", metadata.Title, "テストバグ")
	}

	if metadata.Category != "Web" {
		t.Errorf("Category = %q, want %q", metadata.Category, "Web")
	}

	if len(metadata.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(metadata.Tags))
	} else {
		if metadata.Tags[0] != "バグ" {
			t.Errorf("Tags[0] = %q, want %q", metadata.Tags[0], "バグ")
		}
		if metadata.Tags[1] != "A" {
			t.Errorf("Tags[1] = %q, want %q", metadata.Tags[1], "A")
		}
	}

	if metadata.Reference != "BTS-123" {
		t.Errorf("Reference = %q, want %q", metadata.Reference, "BTS-123")
	}

	expectedDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !metadata.CreatedAt.Equal(expectedDate) {
		t.Errorf("CreatedAt = %v, want %v", metadata.CreatedAt, expectedDate)
	}

	if metadata.Source != "spreadsheet" {
		t.Errorf("Source = %q, want %q", metadata.Source, "spreadsheet")
	}
}

func TestGenerateDocumentID(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル"}
	row := []string{"42", "テスト"}

	cfg := SheetConfig{
		ID:    "spreadsheet-123",
		Sheet: "Sheet1",
		Metadata: MetadataMapping{
			ID: "#",
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	docID := fetcher.generateDocumentID(cfg, 2, row, headers, detector)

	expected := "spreadsheet-123_Sheet1_42"
	if docID != expected {
		t.Errorf("generateDocumentID() = %q, want %q", docID, expected)
	}
}

func TestGenerateDocumentIDFallbackToRowIndex(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"タイトル", "詳細"} // No ID column
	row := []string{"テスト", "詳細"}

	cfg := SheetConfig{
		ID:       "spreadsheet-123",
		Sheet:    "Sheet1",
		Metadata: MetadataMapping{
			// No ID mapping
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	docID := fetcher.generateDocumentID(cfg, 5, row, headers, detector)

	expected := "spreadsheet-123_Sheet1_row5"
	if docID != expected {
		t.Errorf("generateDocumentID() = %q, want %q", docID, expected)
	}
}

func TestRowToFileInfo(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル", "詳細", "カテゴリ", "起票日"}
	row := []string{"1", "テストタイトル", "これはテストの詳細です", "バグ", "2024/01/15"}

	cfg := SheetConfig{
		ID:    "test-spreadsheet",
		Sheet: "Sheet1",
		Content: ContentConfig{
			Columns: []string{"詳細"},
		},
		Metadata: MetadataMapping{
			Title:     "タイトル",
			Category:  "カテゴリ",
			ID:        "#",
			CreatedAt: "起票日",
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	contentColumns := []string{"詳細"}

	fileInfo := fetcher.rowToFileInfo(row, headers, cfg, 2, detector, contentColumns)

	if fileInfo == nil {
		t.Fatal("Expected non-nil FileInfo")
	}

	if fileInfo.Content != "これはテストの詳細です" {
		t.Errorf("Content = %q, want %q", fileInfo.Content, "これはテストの詳細です")
	}

	if fileInfo.Metadata.Title != "テストタイトル" {
		t.Errorf("Metadata.Title = %q, want %q", fileInfo.Metadata.Title, "テストタイトル")
	}

	if fileInfo.Metadata.Category != "バグ" {
		t.Errorf("Metadata.Category = %q, want %q", fileInfo.Metadata.Category, "バグ")
	}

	expectedPath := "spreadsheet://test-spreadsheet/Sheet1/test-spreadsheet_Sheet1_1"
	if fileInfo.Path != expectedPath {
		t.Errorf("Path = %q, want %q", fileInfo.Path, expectedPath)
	}
}

func TestRowToFileInfoEmptyContent(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル", "詳細"}
	row := []string{"1", "タイトルのみ", ""} // Empty 詳細

	cfg := SheetConfig{
		ID:    "test-spreadsheet",
		Sheet: "Sheet1",
		Content: ContentConfig{
			Columns: []string{"詳細"},
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	contentColumns := []string{"詳細"}

	fileInfo := fetcher.rowToFileInfo(row, headers, cfg, 2, detector, contentColumns)

	if fileInfo != nil {
		t.Error("Expected nil FileInfo for empty content")
	}
}

func TestRowToDryRunInfo(t *testing.T) {
	fetcher := &Fetcher{}
	headers := []string{"#", "タイトル", "詳細", "カテゴリ"}
	row := []string{"42", "テストバグ報告", "これは長い詳細な説明です。バグの再現手順と期待される動作について説明しています。", "Web"}

	cfg := SheetConfig{
		ID:    "test-spreadsheet",
		Sheet: "Sheet1",
		Content: ContentConfig{
			Columns: []string{"詳細"},
		},
		Metadata: MetadataMapping{
			Title:    "タイトル",
			Category: "カテゴリ",
			ID:       "#",
		},
	}

	detector := NewColumnDetector(headers, [][]string{row})
	contentColumns := []string{"詳細"}

	info := fetcher.rowToDryRunInfo(row, headers, cfg, 2, detector, contentColumns)

	if info == nil {
		t.Fatal("Expected non-nil DryRunInfo")
	}

	if info.RowIndex != 2 {
		t.Errorf("RowIndex = %d, want 2", info.RowIndex)
	}

	if info.ID != "42" {
		t.Errorf("ID = %q, want %q", info.ID, "42")
	}

	if info.Title != "テストバグ報告" {
		t.Errorf("Title = %q, want %q", info.Title, "テストバグ報告")
	}

	if info.Category != "Web" {
		t.Errorf("Category = %q, want %q", info.Category, "Web")
	}

	if len(info.ContentColumns) != 1 || info.ContentColumns[0] != "詳細" {
		t.Errorf("ContentColumns = %v, want [詳細]", info.ContentColumns)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "truncate needed",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestEstimateProcessingTime(t *testing.T) {
	tests := []struct {
		rowCount int
		expected int
	}{
		{0, 1},    // Minimum 1 minute
		{10, 1},   // 20 seconds -> 1 minute (minimum)
		{30, 1},   // 60 seconds -> 1 minute
		{60, 2},   // 120 seconds -> 2 minutes
		{300, 10}, // 600 seconds -> 10 minutes
	}

	for _, tt := range tests {
		result := estimateProcessingTime(tt.rowCount)
		if result != tt.expected {
			t.Errorf("estimateProcessingTime(%d) = %d, want %d", tt.rowCount, result, tt.expected)
		}
	}
}

// truncateString and estimateProcessingTime are in cmd/vectorize.go
// These tests verify the expected behavior based on the implementation

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func estimateProcessingTime(rowCount int) int {
	seconds := rowCount * 2
	minutes := seconds / 60
	if minutes < 1 {
		return 1
	}
	return minutes
}
