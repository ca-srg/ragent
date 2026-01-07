package spreadsheet

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// SheetsService wraps the Google Sheets API service
type SheetsService struct {
	service *sheets.Service
}

// NewSheetsService creates a new Google Sheets API client
// using Workload Identity Federation or service account credentials
func NewSheetsService(ctx context.Context, credentialsFile string) (*SheetsService, error) {
	var service *sheets.Service
	var err error

	if credentialsFile != "" {
		// Load credentials from file (Workload Identity Federation or Service Account)
		service, err = newServiceFromCredentialsFile(ctx, credentialsFile)
	} else {
		// Fall back to Application Default Credentials
		service, err = newServiceFromDefaultCredentials(ctx)
	}

	if err != nil {
		return nil, err
	}

	return &SheetsService{service: service}, nil
}

// newServiceFromCredentialsFile creates a Sheets service from a credentials file
func newServiceFromCredentialsFile(ctx context.Context, credentialsFile string) (*sheets.Service, error) {
	// Read credentials file
	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Parse credentials - supports both Workload Identity Federation and Service Account
	creds, err := google.CredentialsFromJSON(ctx, data, sheets.SpreadsheetsReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Create Sheets service with the credentials
	service, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create Sheets service: %w", err)
	}

	return service, nil
}

// newServiceFromDefaultCredentials creates a Sheets service using Application Default Credentials
func newServiceFromDefaultCredentials(ctx context.Context) (*sheets.Service, error) {
	// Use Application Default Credentials
	// This will automatically use GOOGLE_APPLICATION_CREDENTIALS env var if set,
	// or use the default service account in GCP environments
	service, err := sheets.NewService(ctx, option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("failed to create Sheets service with default credentials: %w", err)
	}

	return service, nil
}

// GetSpreadsheet retrieves spreadsheet metadata
func (s *SheetsService) GetSpreadsheet(ctx context.Context, spreadsheetID string) (*sheets.Spreadsheet, error) {
	spreadsheet, err := s.service.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get spreadsheet %s: %w", spreadsheetID, err)
	}
	return spreadsheet, nil
}

// GetSheetData retrieves all data from a specific sheet
func (s *SheetsService) GetSheetData(ctx context.Context, spreadsheetID, sheetName string) (*sheets.ValueRange, error) {
	// Use A:ZZ range to get all columns
	readRange := fmt.Sprintf("'%s'!A:ZZ", sheetName)

	resp, err := s.service.Spreadsheets.Values.Get(spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get sheet data for %s/%s: %w", spreadsheetID, sheetName, err)
	}

	return resp, nil
}

// GetSheetNames returns all sheet names in a spreadsheet
func (s *SheetsService) GetSheetNames(ctx context.Context, spreadsheetID string) ([]string, error) {
	spreadsheet, err := s.GetSpreadsheet(ctx, spreadsheetID)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(spreadsheet.Sheets))
	for i, sheet := range spreadsheet.Sheets {
		names[i] = sheet.Properties.Title
	}

	return names, nil
}

// ValidateConnection tests the connection by attempting to get spreadsheet metadata
func (s *SheetsService) ValidateConnection(ctx context.Context, spreadsheetID string) error {
	_, err := s.GetSpreadsheet(ctx, spreadsheetID)
	if err != nil {
		return fmt.Errorf("failed to validate connection: %w", err)
	}
	return nil
}
