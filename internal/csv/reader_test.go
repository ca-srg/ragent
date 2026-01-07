package csv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReader_ReadFile(t *testing.T) {
	// Create a temporary CSV file
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `ID,タイトル,本文,カテゴリ
1,テスト1,これは本文1です,A
2,テスト2,これは本文2です,B
3,テスト3,これは本文3です,C
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil) // Use default config
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to read CSV file: %v", err)
	}

	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}

	// Check first file
	if files[0].IsCSV != true {
		t.Error("expected IsCSV to be true")
	}
	if files[0].IsMarkdown != false {
		t.Error("expected IsMarkdown to be false")
	}
	if files[0].CSVRowIndex != 2 { // Row 2 (1-based, excluding header)
		t.Errorf("expected CSVRowIndex to be 2, got %d", files[0].CSVRowIndex)
	}
}

func TestReader_ReadFile_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `id,name,description,type
1,Item1,This is a description,TypeA
2,Item2,Another description,TypeB
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	// Create config with explicit column mappings
	autoDetect := false
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern: "*.csv",
					Content: ContentConfig{
						Columns:    []string{"description"},
						AutoDetect: &autoDetect,
					},
					Metadata: MetadataMapping{
						Title:    "name",
						Category: "type",
						ID:       "id",
					},
				},
			},
		},
	}

	reader := NewReader(cfg)
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to read CSV file: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}

	// Check metadata extraction
	if files[0].Metadata.Title != "Item1" {
		t.Errorf("expected title 'Item1', got '%s'", files[0].Metadata.Title)
	}
	if files[0].Metadata.Category != "TypeA" {
		t.Errorf("expected category 'TypeA', got '%s'", files[0].Metadata.Category)
	}

	// Check content
	if files[0].Content != "This is a description" {
		t.Errorf("expected content 'This is a description', got '%s'", files[0].Content)
	}
}

func TestReader_ReadFile_WithTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `title,body
Title1,Body1
Title2,Body2
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	autoDetect := false
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern: "*.csv",
					Content: ContentConfig{
						Columns:    []string{"body"},
						Template:   "# {{title}}\n\n{{body}}",
						AutoDetect: &autoDetect,
					},
				},
			},
		},
	}

	reader := NewReader(cfg)
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to read CSV file: %v", err)
	}

	expectedContent := "# Title1\n\nBody1"
	if files[0].Content != expectedContent {
		t.Errorf("expected content '%s', got '%s'", expectedContent, files[0].Content)
	}
}

func TestReader_ReadFile_MultiplePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two different CSV files
	importantPath := filepath.Join(tmpDir, "important_data.csv")
	samplePath := filepath.Join(tmpDir, "sample_escalation.csv")

	importantContent := `id,タイトル,詳細
1,重要事項1,これは重要な詳細です
`
	sampleContent := `エスカレタイトル,スレッド内容,サマリ
テスト相談,スレッドの内容です,要約です
`

	if err := os.WriteFile(importantPath, []byte(importantContent), 0644); err != nil {
		t.Fatalf("failed to write important CSV: %v", err)
	}
	if err := os.WriteFile(samplePath, []byte(sampleContent), 0644); err != nil {
		t.Fatalf("failed to write sample CSV: %v", err)
	}

	autoDetect := false
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern: "important*.csv",
					Content: ContentConfig{
						Columns:    []string{"詳細"},
						AutoDetect: &autoDetect,
					},
					Metadata: MetadataMapping{
						Title: "タイトル",
						ID:    "id",
					},
				},
				{
					Pattern: "sample*.csv",
					Content: ContentConfig{
						Columns:    []string{"スレッド内容", "サマリ"},
						AutoDetect: &autoDetect,
					},
					Metadata: MetadataMapping{
						Title: "エスカレタイトル",
					},
				},
			},
		},
	}

	reader := NewReader(cfg)

	// Read important file
	importantFiles, err := reader.ReadFile(importantPath)
	if err != nil {
		t.Fatalf("failed to read important CSV: %v", err)
	}
	if len(importantFiles) != 1 {
		t.Fatalf("expected 1 file from important CSV, got %d", len(importantFiles))
	}
	if importantFiles[0].Content != "これは重要な詳細です" {
		t.Errorf("unexpected content: %s", importantFiles[0].Content)
	}
	if importantFiles[0].Metadata.Title != "重要事項1" {
		t.Errorf("unexpected title: %s", importantFiles[0].Metadata.Title)
	}

	// Read sample file
	sampleFiles, err := reader.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("failed to read sample CSV: %v", err)
	}
	if len(sampleFiles) != 1 {
		t.Fatalf("expected 1 file from sample CSV, got %d", len(sampleFiles))
	}
	// Content should be joined with \n\n
	expectedContent := "スレッドの内容です\n\n要約です"
	if sampleFiles[0].Content != expectedContent {
		t.Errorf("expected content '%s', got '%s'", expectedContent, sampleFiles[0].Content)
	}
	if sampleFiles[0].Metadata.Title != "テスト相談" {
		t.Errorf("unexpected title: %s", sampleFiles[0].Metadata.Title)
	}
}

func TestReader_ReadFile_NoMatchingPattern(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "other.csv")

	csvContent := `a,b,c
1,2,3
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{Pattern: "specific.csv"},
			},
		},
	}

	reader := NewReader(cfg)
	_, err := reader.ReadFile(csvPath)
	if err == nil {
		t.Error("expected error for no matching pattern")
	}
	if !strings.Contains(err.Error(), "no configuration found") {
		t.Errorf("expected 'no configuration found' error, got: %v", err)
	}
}

func TestReader_ReadFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "empty.csv")

	if err := os.WriteFile(csvPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil)
	_, err := reader.ReadFile(csvPath)
	if err == nil {
		t.Error("expected error for empty CSV file")
	}
}

func TestReader_ReadFile_NoHeader(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "noheader.csv")

	// All empty headers
	csvContent := `,,
a,b,c
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil)
	_, err := reader.ReadFile(csvPath)
	if err == nil {
		t.Error("expected error for invalid header row")
	}
	if !strings.Contains(err.Error(), "invalid CSV header") {
		t.Errorf("expected 'invalid CSV header' error, got: %v", err)
	}
}

func TestReader_ReadFile_HeaderOnly(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "headeronly.csv")

	csvContent := `id,title,content
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil)
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected nil or empty files for header-only CSV, got %d files", len(files))
	}
}

func TestReader_ReadFile_SkipEmptyRows(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `id,title,content
1,Title1,Content1
,,
2,Title2,Content2
   ,   ,   
3,Title3,Content3
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil)
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to read CSV file: %v", err)
	}

	// Should have 3 files (empty rows skipped)
	if len(files) != 3 {
		t.Errorf("expected 3 files (empty rows skipped), got %d", len(files))
	}
}

func TestReader_ReadFile_NoContentColumns(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	// CSV with short columns that won't be detected as content
	csvContent := `a,b,c
1,2,3
4,5,6
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	// Disable auto-detect and don't specify columns
	autoDetect := false
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern: "*.csv",
					Content: ContentConfig{
						Columns:    []string{},
						AutoDetect: &autoDetect,
					},
				},
			},
		},
	}

	reader := NewReader(cfg)
	_, err := reader.ReadFile(csvPath)
	if err == nil {
		t.Error("expected error for no content columns")
	}
	if !strings.Contains(err.Error(), "no content columns") {
		t.Errorf("expected 'no content columns' error, got: %v", err)
	}
}

func TestReader_ReadFile_FileNotFound(t *testing.T) {
	reader := NewReader(nil)
	_, err := reader.ReadFile("/nonexistent/file.csv")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReader_GetDetectedColumns(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `ID,タイトル,本文,カテゴリ
1,テスト1,これは本文1です,A
2,テスト2,これは本文2です,B
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	reader := NewReader(nil)
	info, err := reader.GetDetectedColumns(csvPath)
	if err != nil {
		t.Fatalf("failed to get detected columns: %v", err)
	}

	if len(info.Headers) != 4 {
		t.Errorf("expected 4 headers, got %d", len(info.Headers))
	}

	if info.TotalRows != 2 {
		t.Errorf("expected 2 total rows, got %d", info.TotalRows)
	}

	if len(info.ContentColumns) == 0 {
		t.Error("expected content columns to be detected")
	}

	if info.TitleColumn != "タイトル" {
		t.Errorf("expected title column 'タイトル', got '%s'", info.TitleColumn)
	}

	if info.CategoryColumn != "カテゴリ" {
		t.Errorf("expected category column 'カテゴリ', got '%s'", info.CategoryColumn)
	}

	if info.IDColumn != "ID" {
		t.Errorf("expected ID column 'ID', got '%s'", info.IDColumn)
	}

	if info.MatchedPattern != "*.csv" {
		t.Errorf("expected matched pattern '*.csv', got '%s'", info.MatchedPattern)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"YYYY/MM/DD", "2024/01/15", false},
		{"YYYY-MM-DD", "2024-01-15", false},
		{"YYYY/M/D", "2024/1/5", false},
		{"YYYY-M-D", "2024-1-5", false},
		{"with time slash", "2024/01/15 10:30:00", false},
		{"with time dash", "2024-01-15 10:30:00", false},
		{"invalid format", "15/01/2024", true},
		{"not a date", "hello", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDate(tc.input)
			if tc.expectErr && err == nil {
				t.Error("expected error")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple.csv", "simple"},
		{"path/to/file.csv", "file"},
		{"file with spaces.csv", "file_with_spaces"},
		{"file.name.csv", "file_name"},
	}

	for _, tc := range tests {
		result := sanitizeFilename(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeFilename(%s): expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"  padded  ", "padded"},
		{"with spaces", "with_spaces"},
		{"with/slash", "with_slash"},
		{"with\\backslash", "with_backslash"},
	}

	for _, tc := range tests {
		result := sanitizeID(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeID(%s): expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestIsEmptyRow(t *testing.T) {
	tests := []struct {
		name     string
		row      []string
		expected bool
	}{
		{"all empty", []string{"", "", ""}, true},
		{"all whitespace", []string{"  ", "\t", "  \n  "}, true},
		{"has content", []string{"", "value", ""}, false},
		{"empty slice", []string{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isEmptyRow(tc.row)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetColumnValue(t *testing.T) {
	headers := []string{"ID", "Name", "Value"}
	row := []string{"1", "Test", "100"}

	tests := []struct {
		name       string
		columnName string
		expected   string
	}{
		{"exact match", "ID", "1"},
		{"case insensitive", "name", "Test"},
		{"not found", "Category", ""},
		{"empty column name", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getColumnValue(row, headers, tc.columnName)
			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestReader_ReadFile_WithHeaderRow(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	// CSV with metadata rows before the actual header
	// Rows 1-6 are metadata/summary rows, row 7 is the header
	csvContent := `計算,平均,WinTicket
テスト規模,dummy,QC期間
社外障害,社外あり,インシデント
計算範囲,現在,リリース前通し
計上年,2025,info
計上年月,date,info
含む,施作,機能
○,QC/テスト1,レース
○,QC/テスト2,開発
○,QC/テスト3,チェックイン
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	// Create config with header_row: 7
	autoDetect := false
	headerRow := 7
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern:   "*.csv",
					HeaderRow: &headerRow,
					Content: ContentConfig{
						Columns:    []string{"施作"},
						AutoDetect: &autoDetect,
					},
					Metadata: MetadataMapping{
						Title:    "施作",
						Category: "機能",
					},
				},
			},
		},
	}

	reader := NewReader(cfg)
	files, err := reader.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("failed to read CSV file: %v", err)
	}

	// Should have 3 data rows (rows 8, 9, 10)
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}

	// Check first row
	if files[0].Metadata.Title != "QC/テスト1" {
		t.Errorf("expected title 'QC/テスト1', got '%s'", files[0].Metadata.Title)
	}
	if files[0].Metadata.Category != "レース" {
		t.Errorf("expected category 'レース', got '%s'", files[0].Metadata.Category)
	}

	// Check row indices are correct (actual row numbers in the CSV file)
	if files[0].CSVRowIndex != 8 {
		t.Errorf("expected CSVRowIndex 8, got %d", files[0].CSVRowIndex)
	}
	if files[1].CSVRowIndex != 9 {
		t.Errorf("expected CSVRowIndex 9, got %d", files[1].CSVRowIndex)
	}
	if files[2].CSVRowIndex != 10 {
		t.Errorf("expected CSVRowIndex 10, got %d", files[2].CSVRowIndex)
	}
}

func TestReader_ReadFile_HeaderRowExceedsTotal(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	csvContent := `a,b,c
1,2,3
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	// header_row: 10 exceeds the total rows (2)
	headerRow := 10
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern:   "*.csv",
					HeaderRow: &headerRow,
					Content: ContentConfig{
						Columns: []string{"a"},
					},
				},
			},
		},
	}

	reader := NewReader(cfg)
	_, err := reader.ReadFile(csvPath)
	if err == nil {
		t.Error("expected error for header_row exceeding total rows")
	}
	if !strings.Contains(err.Error(), "exceeds total rows") {
		t.Errorf("expected 'exceeds total rows' error, got: %v", err)
	}
}

func TestReader_GetDetectedColumns_WithHeaderRow(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")

	// CSV with metadata rows (all rows have same number of columns)
	csvContent := `summary,info,extra
meta1,meta2,meta3
ID,タイトル,本文
1,テスト1,内容1
2,テスト2,内容2
`

	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	headerRow := 3
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{
					Pattern:   "*.csv",
					HeaderRow: &headerRow,
				},
			},
		},
	}

	reader := NewReader(cfg)
	info, err := reader.GetDetectedColumns(csvPath)
	if err != nil {
		t.Fatalf("failed to get detected columns: %v", err)
	}

	// Headers should be from row 3
	if len(info.Headers) != 3 {
		t.Errorf("expected 3 headers, got %d", len(info.Headers))
	}
	if info.Headers[0] != "ID" {
		t.Errorf("expected first header 'ID', got '%s'", info.Headers[0])
	}

	// Total rows should be data rows only (2)
	if info.TotalRows != 2 {
		t.Errorf("expected 2 total rows (data only), got %d", info.TotalRows)
	}

	// HeaderRow should be reported
	if info.HeaderRow != 3 {
		t.Errorf("expected HeaderRow 3, got %d", info.HeaderRow)
	}
}
