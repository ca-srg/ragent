package hashstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHashStore(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	assert.NotNil(t, store)
	assert.FileExists(t, dbPath)
}

func TestHashStore_UpsertAndGetFileHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Test insert
	record := &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/to/file.md",
		ContentHash:  "abc123def456",
		FileSize:     1024,
		VectorizedAt: time.Now(),
	}

	err = store.UpsertFileHash(ctx, record)
	require.NoError(t, err)

	// Test get
	retrieved, err := store.GetFileHash(ctx, "local", "/path/to/file.md")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, "local", retrieved.SourceType)
	assert.Equal(t, "/path/to/file.md", retrieved.FilePath)
	assert.Equal(t, "abc123def456", retrieved.ContentHash)
	assert.Equal(t, int64(1024), retrieved.FileSize)

	// Test update (upsert with same key)
	record.ContentHash = "newHash789"
	record.FileSize = 2048
	err = store.UpsertFileHash(ctx, record)
	require.NoError(t, err)

	retrieved, err = store.GetFileHash(ctx, "local", "/path/to/file.md")
	require.NoError(t, err)
	assert.Equal(t, "newHash789", retrieved.ContentHash)
	assert.Equal(t, int64(2048), retrieved.FileSize)
}

func TestHashStore_GetFileHash_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Test get non-existent record
	retrieved, err := store.GetFileHash(ctx, "local", "/nonexistent/file.md")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestHashStore_GetAllFileHashes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Insert multiple records
	records := []*FileHashRecord{
		{SourceType: "local", FilePath: "/path/file1.md", ContentHash: "hash1", FileSize: 100, VectorizedAt: time.Now()},
		{SourceType: "local", FilePath: "/path/file2.md", ContentHash: "hash2", FileSize: 200, VectorizedAt: time.Now()},
		{SourceType: "s3", FilePath: "s3://bucket/file3.md", ContentHash: "hash3", FileSize: 300, VectorizedAt: time.Now()},
	}

	for _, r := range records {
		err = store.UpsertFileHash(ctx, r)
		require.NoError(t, err)
	}

	// Get all local files
	localHashes, err := store.GetAllFileHashes(ctx, "local")
	require.NoError(t, err)
	assert.Len(t, localHashes, 2)
	assert.Contains(t, localHashes, "/path/file1.md")
	assert.Contains(t, localHashes, "/path/file2.md")

	// Get all S3 files
	s3Hashes, err := store.GetAllFileHashes(ctx, "s3")
	require.NoError(t, err)
	assert.Len(t, s3Hashes, 1)
	assert.Contains(t, s3Hashes, "s3://bucket/file3.md")
}

func TestHashStore_DeleteFileHash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Insert a record
	record := &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/to/delete.md",
		ContentHash:  "hashToDelete",
		FileSize:     512,
		VectorizedAt: time.Now(),
	}
	err = store.UpsertFileHash(ctx, record)
	require.NoError(t, err)

	// Verify it exists
	retrieved, err := store.GetFileHash(ctx, "local", "/path/to/delete.md")
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Delete it
	err = store.DeleteFileHash(ctx, "local", "/path/to/delete.md")
	require.NoError(t, err)

	// Verify it's gone
	retrieved, err = store.GetFileHash(ctx, "local", "/path/to/delete.md")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestChangeType_String(t *testing.T) {
	tests := []struct {
		changeType ChangeType
		expected   string
	}{
		{ChangeTypeNone, "none"},
		{ChangeTypeNew, "new"},
		{ChangeTypeModified, "modified"},
		{ChangeTypeDeleted, "deleted"},
		{ChangeType(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.changeType.String())
	}
}

func TestHashStore_GetAllFileHashesForSourceTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hashstore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Insert records for multiple source types
	records := []*FileHashRecord{
		{SourceType: "local", FilePath: "/path/file1.md", ContentHash: "hash1", FileSize: 100, VectorizedAt: time.Now()},
		{SourceType: "local", FilePath: "/path/file2.md", ContentHash: "hash2", FileSize: 200, VectorizedAt: time.Now()},
		{SourceType: "s3", FilePath: "s3://bucket/file3.md", ContentHash: "hash3", FileSize: 300, VectorizedAt: time.Now()},
		{SourceType: "s3", FilePath: "s3://bucket/file4.md", ContentHash: "hash4", FileSize: 400, VectorizedAt: time.Now()},
	}

	for _, r := range records {
		err = store.UpsertFileHash(ctx, r)
		require.NoError(t, err)
	}

	// Get all files for both source types (mixed mode)
	allHashes, err := store.GetAllFileHashesForSourceTypes(ctx, []string{"local", "s3"})
	require.NoError(t, err)
	assert.Len(t, allHashes, 4)
	assert.Contains(t, allHashes, "/path/file1.md")
	assert.Contains(t, allHashes, "/path/file2.md")
	assert.Contains(t, allHashes, "s3://bucket/file3.md")
	assert.Contains(t, allHashes, "s3://bucket/file4.md")

	// Get only local files
	localHashes, err := store.GetAllFileHashesForSourceTypes(ctx, []string{"local"})
	require.NoError(t, err)
	assert.Len(t, localHashes, 2)
	assert.Contains(t, localHashes, "/path/file1.md")
	assert.Contains(t, localHashes, "/path/file2.md")

	// Empty source types should return empty map
	emptyHashes, err := store.GetAllFileHashesForSourceTypes(ctx, []string{})
	require.NoError(t, err)
	assert.Len(t, emptyHashes, 0)
}
