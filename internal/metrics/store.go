package metrics

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Mode represents the type of invocation being tracked.
type Mode string

const (
	ModeMCP   Mode = "mcp"
	ModeSlack Mode = "slack"
	ModeQuery Mode = "query"
	ModeChat  Mode = "chat"
)

// Store manages SQLite persistence for invocation counts.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store with the database at ~/.ragent/stats.db.
// The directory and database file are created if they don't exist.
func NewStore() (*Store, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	ragentDir := filepath.Join(homeDir, ".ragent")
	if err := os.MkdirAll(ragentDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .ragent directory: %w", err)
	}

	dbPath := filepath.Join(ragentDir, "stats.db")
	return NewStoreWithPath(dbPath)
}

// NewStoreWithPath creates a new Store with a custom database path.
// This is useful for testing.
func NewStoreWithPath(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create table if not exists
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS invocation_counts (
			mode TEXT NOT NULL,
			date TEXT NOT NULL,
			count INTEGER DEFAULT 0,
			PRIMARY KEY (mode, date)
		);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return &Store{db: db}, nil
}

// Increment increments the count for the given mode for today's date.
func (s *Store) Increment(mode Mode) error {
	today := time.Now().Format("2006-01-02")

	// Use upsert (INSERT OR REPLACE with increment)
	upsertSQL := `
		INSERT INTO invocation_counts (mode, date, count)
		VALUES (?, ?, 1)
		ON CONFLICT(mode, date) DO UPDATE SET count = count + 1;
	`
	_, err := s.db.Exec(upsertSQL, string(mode), today)
	if err != nil {
		return fmt.Errorf("failed to increment count: %w", err)
	}

	return nil
}

// GetTotalByMode returns the cumulative count for a specific mode across all dates.
func (s *Store) GetTotalByMode(mode Mode) (int64, error) {
	var total int64
	row := s.db.QueryRow(
		"SELECT COALESCE(SUM(count), 0) FROM invocation_counts WHERE mode = ?",
		string(mode),
	)
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("failed to get total for mode %s: %w", mode, err)
	}
	return total, nil
}

// GetAllTotals returns a map of cumulative counts for all modes.
func (s *Store) GetAllTotals() (map[Mode]int64, error) {
	result := make(map[Mode]int64)

	// Initialize all modes to 0
	for _, mode := range []Mode{ModeMCP, ModeSlack, ModeQuery, ModeChat} {
		result[mode] = 0
	}

	rows, err := s.db.Query(
		"SELECT mode, COALESCE(SUM(count), 0) FROM invocation_counts GROUP BY mode",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query totals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var modeStr string
		var total int64
		if err := rows.Scan(&modeStr, &total); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result[Mode(modeStr)] = total
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// GetCountByDate returns the count for a specific mode and date.
func (s *Store) GetCountByDate(mode Mode, date string) (int64, error) {
	var count int64
	row := s.db.QueryRow(
		"SELECT COALESCE(count, 0) FROM invocation_counts WHERE mode = ? AND date = ?",
		string(mode), date,
	)
	if err := row.Scan(&count); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get count: %w", err)
	}
	return count, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
