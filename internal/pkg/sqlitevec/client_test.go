package sqlitevec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/ingestion/domain"
)

// newTestStore creates a SqliteVecStore backed by a temp DB for use in tests.
func newTestStore(t *testing.T) *SqliteVecStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSqliteVecStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNewSqliteVecStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSqliteVecStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Verify DB file was created
	_, statErr := os.Stat(dbPath)
	assert.NoError(t, statErr, "DB file should exist after NewSqliteVecStore")
}

func TestValidateAccess_AutoCreate(t *testing.T) {
	// Use a nested subdirectory that does not yet exist.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.db")
	store, err := NewSqliteVecStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ValidateAccess(context.Background())
	assert.NoError(t, err)
}

func TestStoreVector(t *testing.T) {
	store := newTestStore(t)

	vd := &domain.VectorData{
		ID:        "test-doc-1",
		Embedding: make([]float64, 1024), // 1024-dimension zero vector
		Metadata: domain.DocumentMetadata{
			Title:     "Test Document",
			Category:  "test",
			FilePath:  "/path/to/doc.md",
			Reference: "https://example.com/doc",
			Author:    "tester",
			WordCount: 42,
		},
		Content:   "Test content for verification",
		CreatedAt: time.Now(),
	}

	err := store.StoreVector(context.Background(), vd)
	assert.NoError(t, err, "StoreVector should succeed")

	// Verify by querying the DB directly (white-box access to db field)
	var count int
	queryErr := store.db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM (SELECT key FROM vectors WHERE key = ?)",
		vd.ID,
	).Scan(&count)
	assert.NoError(t, queryErr, "Query should succeed")
	assert.Equal(t, 1, count, "Vector should be present in DB")
}

func TestStoreVector_DuplicateKey(t *testing.T) {
	store := newTestStore(t)

	vd := &domain.VectorData{
		ID:        "dup-doc-1",
		Embedding: make([]float64, 1024),
		Metadata: domain.DocumentMetadata{
			Title: "Original Title",
		},
		Content:   "original content",
		CreatedAt: time.Now(),
	}

	// First insert
	err := store.StoreVector(context.Background(), vd)
	require.NoError(t, err, "first StoreVector should succeed")

	// Update and store again with same key (DELETE + INSERT path)
	vd.Metadata.Title = "Updated Title"
	vd.Content = "updated content"
	err = store.StoreVector(context.Background(), vd)
	assert.NoError(t, err, "second StoreVector with same key should succeed")

	// Verify only one row exists for that key
	var count int
	queryErr := store.db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM (SELECT key FROM vectors WHERE key = ?)",
		vd.ID,
	).Scan(&count)
	assert.NoError(t, queryErr)
	assert.Equal(t, 1, count, "Only one row should exist after duplicate insert")
}

func TestStoreVector_EmptyID(t *testing.T) {
	store := newTestStore(t)

	vd := &domain.VectorData{
		ID:        "",
		Embedding: make([]float64, 1024),
	}

	err := store.StoreVector(context.Background(), vd)
	assert.Error(t, err, "StoreVector with empty ID should return error")
	assert.Contains(t, err.Error(), "empty", "Error message should mention empty ID")
}

// TestStoreVector_NilData verifies nil input is rejected.
func TestStoreVector_NilData(t *testing.T) {
	store := newTestStore(t)
	err := store.StoreVector(context.Background(), nil)
	assert.Error(t, err)
}

// ─── CRUD tests (T5) ───────────────────────────────────────────────────────

// storeTestVector is a helper that persists a vector with a 1024-dim zero
// embedding and the given key/content so CRUD tests can focus on the key.
func storeTestVector(t *testing.T, store *SqliteVecStore, key, content string) {
	t.Helper()
	vd := &domain.VectorData{
		ID:        key,
		Embedding: make([]float64, 1024),
		Metadata:  domain.DocumentMetadata{Title: key},
		Content:   content,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.StoreVector(context.Background(), vd))
}

func TestListVectors_Empty(t *testing.T) {
	store := newTestStore(t)
	keys, err := store.ListVectors(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{}, keys) // NOT nil – empty slice
}

func TestListVectors(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "doc-a", "content a")
	storeTestVector(t, store, "doc-b", "content b")
	storeTestVector(t, store, "other/c", "content c")

	keys, err := store.ListVectors(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestListVectors_WithPrefix(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "doc/a", "content a")
	storeTestVector(t, store, "doc/b", "content b")
	storeTestVector(t, store, "other/c", "content c")

	keys, err := store.ListVectors(context.Background(), "doc/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "doc/a")
	assert.Contains(t, keys, "doc/b")
	assert.NotContains(t, keys, "other/c")
}

func TestListVectors_PrefixSpecialChars(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "docs/100%_complete", "content")
	storeTestVector(t, store, "docs/other", "other")

	// Search with % in prefix – should match the literal %
	keys, err := store.ListVectors(context.Background(), "docs/100%")
	require.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "docs/100%_complete")

	// Search with _ in prefix – should match the literal _
	storeTestVector(t, store, "test_doc", "content")
	keys2, err := store.ListVectors(context.Background(), "test_")
	require.NoError(t, err)
	assert.Len(t, keys2, 1)
	assert.Contains(t, keys2, "test_doc")
}

func TestDeleteVector(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "key-to-delete", "content")

	err := store.DeleteVector(context.Background(), "key-to-delete")
	require.NoError(t, err)

	keys, _ := store.ListVectors(context.Background(), "")
	assert.NotContains(t, keys, "key-to-delete")
}

func TestDeleteVector_NonExistent(t *testing.T) {
	store := newTestStore(t)
	// Deleting a non-existent key should NOT return an error.
	err := store.DeleteVector(context.Background(), "nonexistent-key")
	assert.NoError(t, err)
}

func TestDeleteAllVectors(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "key-1", "content 1")
	storeTestVector(t, store, "key-2", "content 2")
	storeTestVector(t, store, "key-3", "content 3")

	count, err := store.DeleteAllVectors(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	keys, _ := store.ListVectors(context.Background(), "")
	assert.Empty(t, keys)
}

func TestDeleteAllVectors_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	count, err := store.DeleteAllVectors(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetBackendInfo(t *testing.T) {
	store := newTestStore(t)
	storeTestVector(t, store, "key-1", "content")

	info, err := store.GetBackendInfo(context.Background())
	require.NoError(t, err)

	assert.NotNil(t, info["db_path"])
	assert.NotNil(t, info["file_size_bytes"])
	assert.Equal(t, 1, info["vector_count"])
	assert.Equal(t, "sqlite", info["backend"])
}
