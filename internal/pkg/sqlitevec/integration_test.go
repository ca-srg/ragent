package sqlitevec_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/ingestion/domain"
	"github.com/ca-srg/ragent/internal/pkg/sqlitevec"
)

func makeEmbedding(seed float64) []float64 {
	emb := make([]float64, 1024)
	for i := range emb {
		emb[i] = seed + float64(i)*0.001
	}
	return emb
}

// TestIntegration_SqliteVecFullLifecycle exercises the full Store → List →
// ListWithPrefix → Delete → DeleteAll → GetBackendInfo lifecycle against a
// real SQLite database in a temp directory.
func TestIntegration_SqliteVecFullLifecycle(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "integration.db")

	// ── Step 1: Create store ──────────────────────────────────────────────────
	store, err := sqlitevec.NewSqliteVecStore(dbPath)
	require.NoError(t, err, "NewSqliteVecStore should succeed")
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()

	// ── Step 2: Store 5 VectorData items ─────────────────────────────────────
	type vectorSpec struct {
		id       string
		seed     float64
		metadata domain.DocumentMetadata
		content  string
	}

	items := []vectorSpec{
		{
			id:   "docs/guide-1",
			seed: 0.1,
			metadata: domain.DocumentMetadata{
				Title:     "Getting Started Guide",
				Category:  "documentation",
				FilePath:  "docs/guide-1.md",
				Reference: "https://example.com/docs/guide-1",
				Author:    "alice",
				WordCount: 512,
			},
			content: "Introduction to the system. This guide walks you through the initial setup.",
		},
		{
			id:   "docs/guide-2",
			seed: 0.2,
			metadata: domain.DocumentMetadata{
				Title:     "Advanced Configuration",
				Category:  "documentation",
				FilePath:  "docs/guide-2.md",
				Reference: "https://example.com/docs/guide-2",
				Author:    "bob",
				WordCount: 1024,
			},
			content: "Advanced topics including custom plugins and environment tuning.",
		},
		{
			id:   "docs/guide-3",
			seed: 0.3,
			metadata: domain.DocumentMetadata{
				Title:     "Troubleshooting",
				Category:  "documentation",
				FilePath:  "docs/guide-3.md",
				Reference: "https://example.com/docs/guide-3",
				Author:    "carol",
				WordCount: 768,
			},
			content: "Common issues and their resolutions.",
		},
		{
			id:   "api/endpoint-1",
			seed: 0.4,
			metadata: domain.DocumentMetadata{
				Title:     "Search API Reference",
				Category:  "api",
				FilePath:  "api/endpoint-1.md",
				Reference: "https://example.com/api/search",
				Author:    "dave",
				WordCount: 300,
			},
			content: "GET /api/v1/search — returns ranked documents matching the query.",
		},
		{
			id:   "api/endpoint-2",
			seed: 0.5,
			metadata: domain.DocumentMetadata{
				Title:     "Vectorize API Reference",
				Category:  "api",
				FilePath:  "api/endpoint-2.md",
				Reference: "https://example.com/api/vectorize",
				Author:    "eve",
				WordCount: 280,
			},
			content: "POST /api/v1/vectorize — triggers vectorization of the source directory.",
		},
	}

	for _, item := range items {
		vd := &domain.VectorData{
			ID:        item.id,
			Embedding: makeEmbedding(item.seed),
			Metadata:  item.metadata,
			Content:   item.content,
			CreatedAt: time.Now(),
		}
		err := store.StoreVector(ctx, vd)
		require.NoError(t, err, "StoreVector(%q) should succeed", item.id)
	}

	// ── Step 3: ListVectors("") → 5 keys ─────────────────────────────────────
	allKeys, err := store.ListVectors(ctx, "")
	require.NoError(t, err)
	assert.Len(t, allKeys, 5, "should have 5 vectors after storing 5 items")
	assert.Contains(t, allKeys, "docs/guide-1")
	assert.Contains(t, allKeys, "docs/guide-2")
	assert.Contains(t, allKeys, "docs/guide-3")
	assert.Contains(t, allKeys, "api/endpoint-1")
	assert.Contains(t, allKeys, "api/endpoint-2")

	// ── Step 4: ListVectors("docs/") → 3 keys ────────────────────────────────
	docsKeys, err := store.ListVectors(ctx, "docs/")
	require.NoError(t, err)
	assert.Len(t, docsKeys, 3, "docs/ prefix should match 3 vectors")
	assert.Contains(t, docsKeys, "docs/guide-1")
	assert.Contains(t, docsKeys, "docs/guide-2")
	assert.Contains(t, docsKeys, "docs/guide-3")
	assert.NotContains(t, docsKeys, "api/endpoint-1")
	assert.NotContains(t, docsKeys, "api/endpoint-2")

	// ── Step 5: ListVectors("api/") → 2 keys ─────────────────────────────────
	apiKeys, err := store.ListVectors(ctx, "api/")
	require.NoError(t, err)
	assert.Len(t, apiKeys, 2, "api/ prefix should match 2 vectors")
	assert.Contains(t, apiKeys, "api/endpoint-1")
	assert.Contains(t, apiKeys, "api/endpoint-2")

	// ── Step 6: DeleteVector("docs/guide-1") → nil error ─────────────────────
	err = store.DeleteVector(ctx, "docs/guide-1")
	require.NoError(t, err, "DeleteVector should succeed")

	// ── Step 7: ListVectors("") → 4 keys ─────────────────────────────────────
	keysAfterDelete, err := store.ListVectors(ctx, "")
	require.NoError(t, err)
	assert.Len(t, keysAfterDelete, 4, "should have 4 vectors after deleting one")
	assert.NotContains(t, keysAfterDelete, "docs/guide-1", "deleted key should be absent")

	// ── Step 8: DeleteAllVectors() → (4, nil) ────────────────────────────────
	deleted, err := store.DeleteAllVectors(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, deleted, "DeleteAllVectors should report 4 rows removed")

	// ── Step 9: ListVectors("") → 0 keys ─────────────────────────────────────
	emptyKeys, err := store.ListVectors(ctx, "")
	require.NoError(t, err)
	assert.Len(t, emptyKeys, 0, "should have 0 vectors after DeleteAll")
	// Empty slice, not nil.
	assert.Equal(t, []string{}, emptyKeys)

	// ── Step 10: Store 2 vectors again ────────────────────────────────────────
	for _, item := range items[:2] {
		vd := &domain.VectorData{
			ID:        item.id,
			Embedding: makeEmbedding(item.seed),
			Metadata:  item.metadata,
			Content:   item.content,
			CreatedAt: time.Now(),
		}
		require.NoError(t, store.StoreVector(ctx, vd), "re-StoreVector(%q) should succeed", item.id)
	}

	// ── Step 11: GetBackendInfo() → validate fields ───────────────────────────
	info, err := store.GetBackendInfo(ctx)
	require.NoError(t, err)

	assert.Equal(t, "sqlite", info["backend"], "backend field should be \"sqlite\"")
	assert.Equal(t, 2, info["vector_count"], "vector_count should reflect 2 stored vectors")

	dbPathVal, ok := info["db_path"]
	require.True(t, ok, "db_path key must be present")
	assert.NotEmpty(t, dbPathVal, "db_path should be non-empty")

	_, hasFileSize := info["file_size_bytes"]
	assert.True(t, hasFileSize, "file_size_bytes key must be present")
}
