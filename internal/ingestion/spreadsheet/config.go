package spreadsheet

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads and validates spreadsheet configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Apply defaults
	applyDefaults(&config)

	return &config, nil
}

// validateConfig validates the configuration
func validateConfig(cfg *Config) error {
	if len(cfg.Spreadsheets) == 0 {
		return fmt.Errorf("at least one spreadsheet must be configured")
	}

	for i, sheet := range cfg.Spreadsheets {
		if sheet.ID == "" {
			return fmt.Errorf("spreadsheet[%d]: id is required", i)
		}
	}

	return nil
}

// applyDefaults applies default values to the configuration
func applyDefaults(cfg *Config) {
	// Check for GOOGLE_APPLICATION_CREDENTIALS if credentials_file is not set
	if cfg.GCP.CredentialsFile == "" {
		cfg.GCP.CredentialsFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}

	// Apply defaults to each spreadsheet config
	for i := range cfg.Spreadsheets {
		sheet := &cfg.Spreadsheets[i]

		// Default sheet name
		if sheet.Sheet == "" {
			sheet.Sheet = "Sheet1"
		}

		// Default auto_detect to true if not explicitly set
		if sheet.Content.AutoDetect == nil {
			autoDetect := true
			sheet.Content.AutoDetect = &autoDetect
		}
	}
}

// GetCredentialsFile returns the credentials file path
// Priority: config file > GOOGLE_APPLICATION_CREDENTIALS env var
func (cfg *Config) GetCredentialsFile() string {
	if cfg.GCP.CredentialsFile != "" {
		return cfg.GCP.CredentialsFile
	}
	return os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
}
