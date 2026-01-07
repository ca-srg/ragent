package spreadsheet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `
gcp:
  credentials_file: "/path/to/creds.json"
spreadsheets:
  - id: "test-spreadsheet-id"
    sheet: "Sheet1"
    content:
      columns: ["詳細", "タイトル"]
      auto_detect: false
    metadata:
      title: "タイトル"
      category: "カテゴリ"
      id: "#"
      created_at: "起票日"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify GCP config
	if cfg.GCP.CredentialsFile != "/path/to/creds.json" {
		t.Errorf("Expected credentials_file '/path/to/creds.json', got: %s", cfg.GCP.CredentialsFile)
	}

	// Verify spreadsheet config
	if len(cfg.Spreadsheets) != 1 {
		t.Fatalf("Expected 1 spreadsheet, got: %d", len(cfg.Spreadsheets))
	}

	sheet := cfg.Spreadsheets[0]
	if sheet.ID != "test-spreadsheet-id" {
		t.Errorf("Expected ID 'test-spreadsheet-id', got: %s", sheet.ID)
	}

	if sheet.Sheet != "Sheet1" {
		t.Errorf("Expected sheet 'Sheet1', got: %s", sheet.Sheet)
	}

	// Verify content config
	if len(sheet.Content.Columns) != 2 {
		t.Errorf("Expected 2 content columns, got: %d", len(sheet.Content.Columns))
	}

	if sheet.Content.IsAutoDetectEnabled() {
		t.Error("Expected auto_detect to be false")
	}

	// Verify metadata mapping
	if sheet.Metadata.Title != "タイトル" {
		t.Errorf("Expected title 'タイトル', got: %s", sheet.Metadata.Title)
	}

	if sheet.Metadata.Category != "カテゴリ" {
		t.Errorf("Expected category 'カテゴリ', got: %s", sheet.Metadata.Category)
	}

	if sheet.Metadata.ID != "#" {
		t.Errorf("Expected ID '#', got: %s", sheet.Metadata.ID)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Create a minimal config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "minimal-config.yaml")

	configContent := `
spreadsheets:
  - id: "test-id"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	sheet := cfg.Spreadsheets[0]

	// Default sheet name should be "Sheet1"
	if sheet.Sheet != "Sheet1" {
		t.Errorf("Expected default sheet 'Sheet1', got: %s", sheet.Sheet)
	}

	// Auto detect should be enabled by default
	if !sheet.Content.IsAutoDetectEnabled() {
		t.Error("Expected auto_detect to be true by default")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name:        "empty spreadsheets",
			content:     "spreadsheets: []",
			expectError: true,
		},
		{
			name: "missing spreadsheet id",
			content: `
spreadsheets:
  - sheet: "Sheet1"
`,
			expectError: true,
		},
		{
			name: "valid config",
			content: `
spreadsheets:
  - id: "valid-id"
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			_, err := LoadConfig(configPath)
			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestGetCredentialsFile(t *testing.T) {
	// Test with credentials_file set in config
	cfg := &Config{
		GCP: GCPConfig{
			CredentialsFile: "/path/to/creds.json",
		},
	}

	if cfg.GetCredentialsFile() != "/path/to/creds.json" {
		t.Errorf("Expected '/path/to/creds.json', got: %s", cfg.GetCredentialsFile())
	}

	// Test fallback to environment variable
	cfg.GCP.CredentialsFile = ""
	originalEnv := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	defer os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", originalEnv)

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/env/path/creds.json")
	if cfg.GetCredentialsFile() != "/env/path/creds.json" {
		t.Errorf("Expected '/env/path/creds.json', got: %s", cfg.GetCredentialsFile())
	}
}

func TestContentConfigIsAutoDetectEnabled(t *testing.T) {
	tests := []struct {
		name       string
		autoDetect *bool
		expected   bool
	}{
		{
			name:       "nil (default true)",
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ContentConfig{AutoDetect: tt.autoDetect}
			if cfg.IsAutoDetectEnabled() != tt.expected {
				t.Errorf("IsAutoDetectEnabled() = %v, want %v", cfg.IsAutoDetectEnabled(), tt.expected)
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
