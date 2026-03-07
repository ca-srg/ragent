// Package sqlitevec provides a SQLite-backed VectorStore implementation.
// It stores embedding vectors and metadata in a local SQLite database using
// a regular table with BLOB storage for embeddings, enabling offline / low-cost
// operation without cloud dependencies.
//
// Driver: modernc.org/sqlite (pure Go, CGO-free, driver name "sqlite").
// Embeddings are serialised as little-endian float32 BLOBs (same format
// that sqlite-vec uses internally), but stored in a standard TEXT/BLOB table
// because vec0 virtual tables require the C extension which conflicts with
// CGO-free goals.
package sqlitevec

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers "sqlite" driver – pure Go, CGO-free

	"github.com/ca-srg/ragent/internal/ingestion/domain"
	"github.com/ca-srg/ragent/internal/ingestion/vectorizer"
)

// Ensure SqliteVecStore implements the VectorStore interface at compile time.
var _ vectorizer.VectorStore = (*SqliteVecStore)(nil)

// createTableSQL defines the schema for vector storage.
// Embeddings are stored as raw little-endian float32 bytes in a BLOB column.
// A regular table is used instead of a vec0 virtual table because vec0 requires
// loading the sqlite-vec C extension, which is incompatible with CGO-free builds.
const createTableSQL = `
CREATE TABLE IF NOT EXISTS vectors (
    key TEXT PRIMARY KEY,
    embedding BLOB NOT NULL,
    title TEXT,
    category TEXT,
    file_path TEXT,
    reference TEXT,
    author TEXT,
    word_count INTEGER DEFAULT 0,
    content_excerpt TEXT,
    created_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_vectors_key ON vectors(key);`

// SqliteVecStore stores embedding vectors in a local SQLite database.
type SqliteVecStore struct {
	db     *sql.DB
	dbPath string
}

// NewSqliteVecStore opens (or creates) a SQLite database at dbPath and
// ensures the schema is initialised. The path may start with "~/" which is
// expanded to the current user's home directory.
func NewSqliteVecStore(dbPath string) (*SqliteVecStore, error) {
	// Expand ~ prefix.
	if strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[2:])
	}

	// Create parent directories if they do not exist.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directories for %s: %w", dbPath, err)
	}

	// Open database using modernc.org/sqlite (driver name "sqlite").
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}

	// Verify the connection is usable before returning.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	store := &SqliteVecStore{db: db, dbPath: dbPath}

	ctx := context.Background()
	if err := store.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Printf("INFO: sqlite-vec store opened at %s", dbPath)
	return store, nil
}

// initSchema enables WAL mode and creates the vectors table if needed.
// It is idempotent and safe to call multiple times.
func (s *SqliteVecStore) initSchema(ctx context.Context) error {
	// WAL mode improves concurrent read performance.
	if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create vectors table: %w", err)
	}

	return nil
}

// StoreVector persists a vector and its associated metadata. If a vector
// with the same ID already exists it is replaced (DELETE + INSERT).
func (s *SqliteVecStore) StoreVector(ctx context.Context, vectorData *domain.VectorData) error {
	if vectorData == nil {
		return fmt.Errorf("vector data cannot be nil")
	}
	if vectorData.ID == "" {
		return fmt.Errorf("vector ID cannot be empty")
	}

	// Convert []float64 → []float32 then serialise as little-endian bytes.
	embedding32 := make([]float32, len(vectorData.Embedding))
	for i, v := range vectorData.Embedding {
		embedding32[i] = float32(v)
	}
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, embedding32); err != nil {
		return fmt.Errorf("failed to serialize embedding for %q: %w", vectorData.ID, err)
	}
	embeddingBytes := buf.Bytes()

	// DELETE existing row first to support upsert behaviour.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM vectors WHERE key = ?", vectorData.ID); err != nil {
		return fmt.Errorf("failed to delete existing vector %q: %w", vectorData.ID, err)
	}

	// Truncate content to a reasonable excerpt length.
	contentExcerpt := vectorData.Content
	if len(contentExcerpt) > 500 {
		contentExcerpt = contentExcerpt[:500]
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vectors
			(key, embedding, title, category, file_path, reference, author, word_count, content_excerpt, created_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		vectorData.ID,
		embeddingBytes,
		vectorData.Metadata.Title,
		vectorData.Metadata.Category,
		vectorData.Metadata.FilePath,
		vectorData.Metadata.Reference,
		vectorData.Metadata.Author,
		vectorData.Metadata.WordCount,
		contentExcerpt,
		vectorData.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert vector %q: %w", vectorData.ID, err)
	}

	return nil
}

// ValidateAccess verifies the database connection and ensures the schema
// exists. It creates the DB file and table if they do not already exist.
func (s *SqliteVecStore) ValidateAccess(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite connection check failed: %w", err)
	}
	if err := s.initSchema(ctx); err != nil {
		return fmt.Errorf("schema initialization failed: %w", err)
	}
	return nil
}

// ListVectors returns the keys of all stored vectors, optionally filtered by a
// key prefix. An empty prefix returns all keys. LIKE special characters in the
// prefix (%, _, \) are escaped so they are treated as literals.
func (s *SqliteVecStore) ListVectors(ctx context.Context, prefix string) ([]string, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if prefix == "" {
		rows, err = s.db.QueryContext(ctx, "SELECT key FROM vectors ORDER BY key")
	} else {
		// Escape LIKE special characters so the prefix is treated literally.
		escapedPrefix := escapeLIKE(prefix)
		rows, err = s.db.QueryContext(ctx,
			"SELECT key FROM vectors WHERE key LIKE ? ESCAPE '\\' ORDER BY key",
			escapedPrefix+"%")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list vectors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Initialise to a non-nil empty slice so callers can use len() == 0 safely.
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan vector key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// escapeLIKE escapes the three special LIKE characters (%, _, \) so a raw
// user-supplied string can be passed as a LIKE pattern prefix.
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// DeleteVector removes the vector with the given ID. Deleting a non-existent
// key is not considered an error.
func (s *SqliteVecStore) DeleteVector(ctx context.Context, vectorID string) error {
	if vectorID == "" {
		return fmt.Errorf("vector ID cannot be empty")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM vectors WHERE key = ?", vectorID)
	if err != nil {
		return fmt.Errorf("failed to delete vector %q: %w", vectorID, err)
	}
	return nil
}

// DeleteAllVectors deletes every row in the vectors table and returns the
// number of rows removed.
func (s *SqliteVecStore) DeleteAllVectors(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM vectors")
	if err != nil {
		return 0, fmt.Errorf("failed to delete all vectors: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return int(count), nil
}

// GetBackendInfo returns diagnostic metadata about the SQLite backend:
// db_path, file_size_bytes, vector_count, and backend name.
func (s *SqliteVecStore) GetBackendInfo(ctx context.Context) (map[string]interface{}, error) {
	// Count stored vectors.
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to get vector count: %w", err)
	}

	// Determine file size (best-effort; zero if the file is not accessible).
	var fileSize int64
	if fi, err := os.Stat(s.dbPath); err == nil {
		fileSize = fi.Size()
	}

	return map[string]interface{}{
		"backend":         "sqlite",
		"db_path":         s.dbPath,
		"file_size_bytes": fileSize,
		"vector_count":    count,
	}, nil
}

// Close releases the underlying database connection.
func (s *SqliteVecStore) Close() error {
	return s.db.Close()
}
