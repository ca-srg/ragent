package hashstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*HashStore, func()) {
	tmpDir, err := os.MkdirTemp("", "detector_test")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewHashStoreWithPath(dbPath)
	require.NoError(t, err)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestChangeDetector_DetectChanges_NewFiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	detector := NewChangeDetector(store)

	// No existing hashes, all files should be new
	files := []*types.FileInfo{
		{Path: "/path/file1.md", ContentHash: "hash1"},
		{Path: "/path/file2.md", ContentHash: "hash2"},
	}

	result, err := detector.DetectChanges(ctx, []string{"local"}, files)
	require.NoError(t, err)

	assert.Equal(t, 2, result.NewCount)
	assert.Equal(t, 0, result.ModCount)
	assert.Equal(t, 0, result.UnchangeCount)
	assert.Equal(t, 0, result.DeleteCount)
	assert.Len(t, result.ToProcess, 2)
}

func TestChangeDetector_DetectChanges_ModifiedFiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hash
	err := store.UpsertFileHash(ctx, &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/file1.md",
		ContentHash:  "oldHash",
		FileSize:     100,
		VectorizedAt: time.Now(),
	})
	require.NoError(t, err)

	detector := NewChangeDetector(store)

	// File with different hash
	files := []*types.FileInfo{
		{Path: "/path/file1.md", ContentHash: "newHash"},
	}

	result, err := detector.DetectChanges(ctx, []string{"local"}, files)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NewCount)
	assert.Equal(t, 1, result.ModCount)
	assert.Equal(t, 0, result.UnchangeCount)
	assert.Equal(t, 0, result.DeleteCount)
	assert.Len(t, result.ToProcess, 1)
	assert.Equal(t, ChangeTypeModified, result.ToProcess[0].ChangeType)
	assert.Equal(t, "oldHash", result.ToProcess[0].OldHash)
	assert.Equal(t, "newHash", result.ToProcess[0].NewHash)
}

func TestChangeDetector_DetectChanges_UnchangedFiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hash
	err := store.UpsertFileHash(ctx, &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/file1.md",
		ContentHash:  "sameHash",
		FileSize:     100,
		VectorizedAt: time.Now(),
	})
	require.NoError(t, err)

	detector := NewChangeDetector(store)

	// File with same hash
	files := []*types.FileInfo{
		{Path: "/path/file1.md", ContentHash: "sameHash"},
	}

	result, err := detector.DetectChanges(ctx, []string{"local"}, files)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NewCount)
	assert.Equal(t, 0, result.ModCount)
	assert.Equal(t, 1, result.UnchangeCount)
	assert.Equal(t, 0, result.DeleteCount)
	assert.Len(t, result.ToProcess, 0)
	assert.Len(t, result.Unchanged, 1)
}

func TestChangeDetector_DetectChanges_DeletedFiles(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hashes for files that will be "deleted"
	err := store.UpsertFileHash(ctx, &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/deleted.md",
		ContentHash:  "deletedHash",
		FileSize:     100,
		VectorizedAt: time.Now(),
	})
	require.NoError(t, err)

	detector := NewChangeDetector(store)

	// Empty file list = all existing files are deleted
	files := []*types.FileInfo{}

	result, err := detector.DetectChanges(ctx, []string{"local"}, files)
	require.NoError(t, err)

	assert.Equal(t, 0, result.NewCount)
	assert.Equal(t, 0, result.ModCount)
	assert.Equal(t, 0, result.UnchangeCount)
	assert.Equal(t, 1, result.DeleteCount)
	assert.Len(t, result.Deleted, 1)
	assert.Contains(t, result.Deleted, "/path/deleted.md")
}

func TestChangeDetector_DetectChanges_MixedChanges(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hashes
	existingRecords := []*FileHashRecord{
		{SourceType: "local", FilePath: "/path/unchanged.md", ContentHash: "hash1", FileSize: 100, VectorizedAt: time.Now()},
		{SourceType: "local", FilePath: "/path/modified.md", ContentHash: "oldHash", FileSize: 200, VectorizedAt: time.Now()},
		{SourceType: "local", FilePath: "/path/deleted.md", ContentHash: "hash3", FileSize: 300, VectorizedAt: time.Now()},
	}
	for _, r := range existingRecords {
		err := store.UpsertFileHash(ctx, r)
		require.NoError(t, err)
	}

	detector := NewChangeDetector(store)

	// Current files: unchanged, modified, and new (deleted is missing)
	files := []*types.FileInfo{
		{Path: "/path/unchanged.md", ContentHash: "hash1"},  // unchanged
		{Path: "/path/modified.md", ContentHash: "newHash"}, // modified
		{Path: "/path/new.md", ContentHash: "hashNew"},      // new
	}

	result, err := detector.DetectChanges(ctx, []string{"local"}, files)
	require.NoError(t, err)

	assert.Equal(t, 1, result.NewCount)
	assert.Equal(t, 1, result.ModCount)
	assert.Equal(t, 1, result.UnchangeCount)
	assert.Equal(t, 1, result.DeleteCount)
	assert.Len(t, result.ToProcess, 2) // new + modified
	assert.Len(t, result.Unchanged, 1)
	assert.Len(t, result.Deleted, 1)
}

func TestChangeDetector_FilterFilesToProcess(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hash for unchanged file
	err := store.UpsertFileHash(ctx, &FileHashRecord{
		SourceType:   "local",
		FilePath:     "/path/unchanged.md",
		ContentHash:  "sameHash",
		FileSize:     100,
		VectorizedAt: time.Now(),
	})
	require.NoError(t, err)

	detector := NewChangeDetector(store)

	files := []*types.FileInfo{
		{Path: "/path/unchanged.md", ContentHash: "sameHash", Size: 100},
		{Path: "/path/new.md", ContentHash: "newHash", Size: 200},
	}

	filtered, changes, err := detector.FilterFilesToProcess(ctx, []string{"local"}, files)
	require.NoError(t, err)

	// Only new file should be in filtered list
	assert.Len(t, filtered, 1)
	assert.Equal(t, "/path/new.md", filtered[0].Path)

	// Changes should show both
	assert.Equal(t, 1, changes.NewCount)
	assert.Equal(t, 1, changes.UnchangeCount)
}

func TestChangeDetector_DetectChanges_MixedSourceTypes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hashes for both local and s3 source types
	existingRecords := []*FileHashRecord{
		{SourceType: "local", FilePath: "/local/file1.md", ContentHash: "localHash1", FileSize: 100, VectorizedAt: time.Now()},
		{SourceType: "s3", FilePath: "s3://bucket/file2.md", ContentHash: "s3Hash1", FileSize: 200, VectorizedAt: time.Now()},
	}
	for _, r := range existingRecords {
		err := store.UpsertFileHash(ctx, r)
		require.NoError(t, err)
	}

	detector := NewChangeDetector(store)

	// Current files include unchanged local, new local, and modified s3
	files := []*types.FileInfo{
		{Path: "/local/file1.md", ContentHash: "localHash1", SourceType: "local"}, // unchanged
		{Path: "/local/new.md", ContentHash: "newLocalHash", SourceType: "local"}, // new local file
		{Path: "s3://bucket/file2.md", ContentHash: "s3Hash2", SourceType: "s3"},  // modified s3 file
		{Path: "s3://bucket/new.md", ContentHash: "newS3Hash", SourceType: "s3"},  // new s3 file
	}

	// Query with both source types (mixed mode)
	result, err := detector.DetectChanges(ctx, []string{"local", "s3"}, files)
	require.NoError(t, err)

	assert.Equal(t, 2, result.NewCount)      // /local/new.md and s3://bucket/new.md
	assert.Equal(t, 1, result.ModCount)      // s3://bucket/file2.md
	assert.Equal(t, 1, result.UnchangeCount) // /local/file1.md
	assert.Equal(t, 0, result.DeleteCount)
	assert.Len(t, result.ToProcess, 3) // 2 new + 1 modified
}

func TestChangeDetector_FilterFilesToProcess_MixedSourceTypes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add existing hashes for both local and s3
	existingRecords := []*FileHashRecord{
		{SourceType: "local", FilePath: "/local/unchanged.md", ContentHash: "hash1", FileSize: 100, VectorizedAt: time.Now()},
		{SourceType: "s3", FilePath: "s3://bucket/unchanged.md", ContentHash: "hash2", FileSize: 200, VectorizedAt: time.Now()},
	}
	for _, r := range existingRecords {
		err := store.UpsertFileHash(ctx, r)
		require.NoError(t, err)
	}

	detector := NewChangeDetector(store)

	files := []*types.FileInfo{
		{Path: "/local/unchanged.md", ContentHash: "hash1", Size: 100, SourceType: "local"},
		{Path: "/local/new.md", ContentHash: "newHash", Size: 200, SourceType: "local"},
		{Path: "s3://bucket/unchanged.md", ContentHash: "hash2", Size: 200, SourceType: "s3"},
		{Path: "s3://bucket/new.md", ContentHash: "newS3Hash", Size: 300, SourceType: "s3"},
	}

	filtered, changes, err := detector.FilterFilesToProcess(ctx, []string{"local", "s3"}, files)
	require.NoError(t, err)

	// Only new files should be in filtered list
	assert.Len(t, filtered, 2)
	paths := []string{filtered[0].Path, filtered[1].Path}
	assert.Contains(t, paths, "/local/new.md")
	assert.Contains(t, paths, "s3://bucket/new.md")

	// Changes should show all
	assert.Equal(t, 2, changes.NewCount)
	assert.Equal(t, 2, changes.UnchangeCount)
}
