package hashstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// HashStore manages SQLite persistence for file hashes
type HashStore struct {
	db *sql.DB
}

// NewHashStore creates a new HashStore with the database at ~/.ragent/stats.db.
// The directory and database file are created if they don't exist.
func NewHashStore() (*HashStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	ragentDir := filepath.Join(homeDir, ".ragent")
	if err := os.MkdirAll(ragentDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .ragent directory: %w", err)
	}

	dbPath := filepath.Join(ragentDir, "stats.db")
	return NewHashStoreWithPath(dbPath)
}

// NewHashStoreWithPath creates a new HashStore with a custom database path.
// This is useful for testing.
func NewHashStoreWithPath(dbPath string) (*HashStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &HashStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return store, nil
}

// NewHashStoreWithDB creates a new HashStore with an existing database connection.
// This allows sharing the database connection with other stores.
func NewHashStoreWithDB(db *sql.DB) (*HashStore, error) {
	store := &HashStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return store, nil
}

// migrate creates the file_hashes table if it doesn't exist
func (s *HashStore) migrate() error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS file_hashes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_type TEXT NOT NULL,
			file_path TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			vectorized_at DATETIME NOT NULL,
			UNIQUE(source_type, file_path)
		);
	`
	if _, err := s.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create file_hashes table: %w", err)
	}

	// Create index for faster lookups
	createIndexSQL := `
		CREATE INDEX IF NOT EXISTS idx_file_hashes_source_path 
		ON file_hashes(source_type, file_path);
	`
	if _, err := s.db.Exec(createIndexSQL); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return nil
}

// GetFileHash retrieves a file hash record by source type and file path
func (s *HashStore) GetFileHash(ctx context.Context, sourceType, filePath string) (*FileHashRecord, error) {
	query := `
		SELECT id, source_type, file_path, content_hash, file_size, vectorized_at
		FROM file_hashes
		WHERE source_type = ? AND file_path = ?
	`
	row := s.db.QueryRowContext(ctx, query, sourceType, filePath)

	var record FileHashRecord
	var vectorizedAt string
	err := row.Scan(
		&record.ID,
		&record.SourceType,
		&record.FilePath,
		&record.ContentHash,
		&record.FileSize,
		&vectorizedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to get file hash: %w", err)
	}

	// Parse timestamp
	record.VectorizedAt, err = time.Parse("2006-01-02 15:04:05", vectorizedAt)
	if err != nil {
		// Try alternative format
		record.VectorizedAt, err = time.Parse(time.RFC3339, vectorizedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse vectorized_at: %w", err)
		}
	}

	return &record, nil
}

// GetAllFileHashes retrieves all file hash records for a given source type
func (s *HashStore) GetAllFileHashes(ctx context.Context, sourceType string) (map[string]*FileHashRecord, error) {
	query := `
		SELECT id, source_type, file_path, content_hash, file_size, vectorized_at
		FROM file_hashes
		WHERE source_type = ?
	`
	rows, err := s.db.QueryContext(ctx, query, sourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to query file hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]*FileHashRecord)
	for rows.Next() {
		var record FileHashRecord
		var vectorizedAt string
		err := rows.Scan(
			&record.ID,
			&record.SourceType,
			&record.FilePath,
			&record.ContentHash,
			&record.FileSize,
			&vectorizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Parse timestamp
		record.VectorizedAt, err = time.Parse("2006-01-02 15:04:05", vectorizedAt)
		if err != nil {
			record.VectorizedAt, _ = time.Parse(time.RFC3339, vectorizedAt)
		}

		result[record.FilePath] = &record
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// UpsertFileHash inserts or updates a file hash record
func (s *HashStore) UpsertFileHash(ctx context.Context, record *FileHashRecord) error {
	upsertSQL := `
		INSERT INTO file_hashes (source_type, file_path, content_hash, file_size, vectorized_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source_type, file_path) DO UPDATE SET
			content_hash = excluded.content_hash,
			file_size = excluded.file_size,
			vectorized_at = excluded.vectorized_at;
	`
	vectorizedAt := record.VectorizedAt.Format("2006-01-02 15:04:05")
	_, err := s.db.ExecContext(ctx, upsertSQL,
		record.SourceType,
		record.FilePath,
		record.ContentHash,
		record.FileSize,
		vectorizedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert file hash: %w", err)
	}

	return nil
}

// GetAllFileHashesForSourceTypes retrieves all file hash records for multiple source types.
// This is useful for mixed mode where both "local" and "s3" sources are used.
func (s *HashStore) GetAllFileHashesForSourceTypes(ctx context.Context, sourceTypes []string) (map[string]*FileHashRecord, error) {
	if len(sourceTypes) == 0 {
		return make(map[string]*FileHashRecord), nil
	}

	// Build query with IN clause
	placeholders := ""
	args := make([]any, len(sourceTypes))
	for i, st := range sourceTypes {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args[i] = st
	}

	query := fmt.Sprintf(`
		SELECT id, source_type, file_path, content_hash, file_size, vectorized_at
		FROM file_hashes
		WHERE source_type IN (%s)
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query file hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]*FileHashRecord)
	for rows.Next() {
		var record FileHashRecord
		var vectorizedAt string
		err := rows.Scan(
			&record.ID,
			&record.SourceType,
			&record.FilePath,
			&record.ContentHash,
			&record.FileSize,
			&vectorizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Parse timestamp
		record.VectorizedAt, err = time.Parse("2006-01-02 15:04:05", vectorizedAt)
		if err != nil {
			record.VectorizedAt, _ = time.Parse(time.RFC3339, vectorizedAt)
		}

		result[record.FilePath] = &record
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// DeleteFileHash deletes a file hash record by source type and file path
func (s *HashStore) DeleteFileHash(ctx context.Context, sourceType, filePath string) error {
	deleteSQL := `DELETE FROM file_hashes WHERE source_type = ? AND file_path = ?`
	_, err := s.db.ExecContext(ctx, deleteSQL, sourceType, filePath)
	if err != nil {
		return fmt.Errorf("failed to delete file hash: %w", err)
	}
	return nil
}

// DeleteFileHashesBySourceType deletes all file hash records for a given source type
func (s *HashStore) DeleteFileHashesBySourceType(ctx context.Context, sourceType string) (int64, error) {
	deleteSQL := `DELETE FROM file_hashes WHERE source_type = ?`
	result, err := s.db.ExecContext(ctx, deleteSQL, sourceType)
	if err != nil {
		return 0, fmt.Errorf("failed to delete file hashes: %w", err)
	}
	return result.RowsAffected()
}

// Close closes the database connection
func (s *HashStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
