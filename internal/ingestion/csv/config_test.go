package csv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "*.csv"
      content:
        columns: ["本文", "詳細"]
        template: "{{タイトル}}\n\n{{本文}}"
        auto_detect: false
      metadata:
        title: "タイトル"
        category: "カテゴリ"
        tags: ["タグ1", "タグ2"]
        id: "ID"
        created_at: "作成日"
        updated_at: "更新日"
        reference: "参照"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify files config
	if len(cfg.CSV.Files) != 1 {
		t.Fatalf("expected 1 file config, got %d", len(cfg.CSV.Files))
	}

	fc := cfg.CSV.Files[0]

	// Verify pattern
	if fc.Pattern != "*.csv" {
		t.Errorf("expected pattern '*.csv', got '%s'", fc.Pattern)
	}

	// Verify content config
	if len(fc.Content.Columns) != 2 {
		t.Errorf("expected 2 content columns, got %d", len(fc.Content.Columns))
	}
	if fc.Content.Columns[0] != "本文" {
		t.Errorf("expected first column to be '本文', got '%s'", fc.Content.Columns[0])
	}
	if fc.Content.Template != "{{タイトル}}\n\n{{本文}}" {
		t.Errorf("unexpected template: %s", fc.Content.Template)
	}
	if fc.Content.IsAutoDetectEnabled() {
		t.Error("auto_detect should be false")
	}

	// Verify metadata config
	if fc.Metadata.Title != "タイトル" {
		t.Errorf("expected title to be 'タイトル', got '%s'", fc.Metadata.Title)
	}
	if fc.Metadata.Category != "カテゴリ" {
		t.Errorf("expected category to be 'カテゴリ', got '%s'", fc.Metadata.Category)
	}
	if len(fc.Metadata.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(fc.Metadata.Tags))
	}
}

func TestLoadConfig_MultiplePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "important*.csv"
      content:
        columns: ["詳細"]
      metadata:
        title: "タイトル"
    - pattern: "sample*.csv"
      content:
        columns: ["スレッド内容", "サマリ"]
      metadata:
        title: "エスカレタイトル"
    - pattern: "*.csv"
      content:
        auto_detect: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.CSV.Files) != 3 {
		t.Fatalf("expected 3 file configs, got %d", len(cfg.CSV.Files))
	}

	// Test GetConfigForFile matching
	fc := cfg.GetConfigForFile("important_data.csv")
	if fc == nil {
		t.Fatal("expected to find config for important_data.csv")
	}
	if fc.Pattern != "important*.csv" {
		t.Errorf("expected pattern 'important*.csv', got '%s'", fc.Pattern)
	}

	fc = cfg.GetConfigForFile("sample_escalation.csv")
	if fc == nil {
		t.Fatal("expected to find config for sample_escalation.csv")
	}
	if fc.Pattern != "sample*.csv" {
		t.Errorf("expected pattern 'sample*.csv', got '%s'", fc.Pattern)
	}

	fc = cfg.GetConfigForFile("other.csv")
	if fc == nil {
		t.Fatal("expected to find config for other.csv")
	}
	if fc.Pattern != "*.csv" {
		t.Errorf("expected pattern '*.csv', got '%s'", fc.Pattern)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Create a minimal config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "*.csv"
      content:
        columns: []
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// auto_detect should default to true
	if !cfg.CSV.Files[0].Content.IsAutoDetectEnabled() {
		t.Error("auto_detect should default to true")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	if err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadConfig_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	// Old format (without files array) should fail
	configContent := `
csv:
  content:
    columns: ["本文"]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for missing files array")
	}
}

func TestLoadConfig_MissingPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - content:
        columns: ["本文"]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestLoadConfig_InvalidPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "[invalid"
      content:
        columns: ["本文"]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestNewDefaultConfig(t *testing.T) {
	cfg := NewDefaultConfig()

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if len(cfg.CSV.Files) != 1 {
		t.Fatalf("expected 1 file config, got %d", len(cfg.CSV.Files))
	}

	if cfg.CSV.Files[0].Pattern != "*.csv" {
		t.Errorf("expected default pattern '*.csv', got '%s'", cfg.CSV.Files[0].Pattern)
	}

	if !cfg.CSV.Files[0].Content.IsAutoDetectEnabled() {
		t.Error("default config should have auto_detect enabled")
	}
}

func TestGetConfigForFile(t *testing.T) {
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{Pattern: "important*.csv"},
				{Pattern: "sample*.csv"},
				{Pattern: "*.csv"},
			},
		},
	}

	tests := []struct {
		filename        string
		expectedPattern string
	}{
		{"important_data.csv", "important*.csv"},
		{"important.csv", "important*.csv"},
		{"sample_test.csv", "sample*.csv"},
		{"sample.csv", "sample*.csv"},
		{"other.csv", "*.csv"},
		{"/path/to/important_data.csv", "important*.csv"}, // Should match basename
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			fc := cfg.GetConfigForFile(tc.filename)
			if fc == nil {
				t.Fatalf("expected to find config for %s", tc.filename)
			}
			if fc.Pattern != tc.expectedPattern {
				t.Errorf("expected pattern '%s', got '%s'", tc.expectedPattern, fc.Pattern)
			}
		})
	}
}

func TestGetConfigForFile_NoMatch(t *testing.T) {
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{Pattern: "specific.csv"},
			},
		},
	}

	fc := cfg.GetConfigForFile("other.csv")
	if fc != nil {
		t.Error("expected nil for non-matching file")
	}
}

func TestHasConfigForFile(t *testing.T) {
	cfg := &Config{
		CSV: CSVConfig{
			Files: []FileConfig{
				{Pattern: "*.csv"},
			},
		},
	}

	if !cfg.HasConfigForFile("test.csv") {
		t.Error("expected true for matching file")
	}

	if cfg.HasConfigForFile("test.txt") {
		t.Error("expected false for non-matching file")
	}
}

func TestContentConfig_IsAutoDetectEnabled(t *testing.T) {
	tests := []struct {
		name       string
		autoDetect *bool
		expected   bool
	}{
		{
			name:       "nil defaults to true",
			autoDetect: nil,
			expected:   true,
		},
		{
			name:       "explicit true",
			autoDetect: boolPtr(true),
			expected:   true,
		},
		{
			name:       "explicit false",
			autoDetect: boolPtr(false),
			expected:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &ContentConfig{
				AutoDetect: tc.autoDetect,
			}
			if cfg.IsAutoDetectEnabled() != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, cfg.IsAutoDetectEnabled())
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func TestFileConfig_GetHeaderRow(t *testing.T) {
	tests := []struct {
		name      string
		headerRow *int
		expected  int
	}{
		{
			name:      "nil defaults to 1",
			headerRow: nil,
			expected:  1,
		},
		{
			name:      "explicit 1",
			headerRow: intPtr(1),
			expected:  1,
		},
		{
			name:      "explicit 7",
			headerRow: intPtr(7),
			expected:  7,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := &FileConfig{
				Pattern:   "*.csv",
				HeaderRow: tc.headerRow,
			}
			if fc.GetHeaderRow() != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, fc.GetHeaderRow())
			}
		})
	}
}

func TestLoadConfig_WithHeaderRow(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "*.csv"
      header_row: 7
      content:
        columns: ["施作", "機能"]
      metadata:
        title: "施作"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	fc := cfg.CSV.Files[0]
	if fc.GetHeaderRow() != 7 {
		t.Errorf("expected header_row 7, got %d", fc.GetHeaderRow())
	}
}

func TestLoadConfig_InvalidHeaderRow(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "csv-config.yaml")

	configContent := `
csv:
  files:
    - pattern: "*.csv"
      header_row: 0
      content:
        columns: ["test"]
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for header_row < 1")
	}
}
