package vectorizer

import (
	"context"
	"strings"
	"sync"
	"testing"

	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type RecordingVectorStore struct {
	*MockVectorStore

	mu            sync.Mutex
	storedVectors []*pkgdomain.VectorData
}

func NewRecordingVectorStore() *RecordingVectorStore {
	return &RecordingVectorStore{
		MockVectorStore: NewMockVectorStore(),
	}
}

func (r *RecordingVectorStore) StoreVector(ctx context.Context, vectorData *pkgdomain.VectorData) error {
	r.mu.Lock()
	r.storedVectors = append(r.storedVectors, cloneVectorData(vectorData))
	r.mu.Unlock()

	return r.MockVectorStore.StoreVector(ctx, vectorData)
}

func (r *RecordingVectorStore) StoredVectors() []*pkgdomain.VectorData {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored := make([]*pkgdomain.VectorData, len(r.storedVectors))
	for i, vectorData := range r.storedVectors {
		stored[i] = cloneVectorData(vectorData)
	}

	return stored
}

func cloneVectorData(vectorData *pkgdomain.VectorData) *pkgdomain.VectorData {
	if vectorData == nil {
		return nil
	}

	cloned := *vectorData
	cloned.Embedding = append([]float64(nil), vectorData.Embedding...)
	cloned.Metadata.Tags = append([]string(nil), vectorData.Metadata.Tags...)
	if vectorData.Metadata.CustomFields != nil {
		cloned.Metadata.CustomFields = make(map[string]interface{}, len(vectorData.Metadata.CustomFields))
		for key, value := range vectorData.Metadata.CustomFields {
			cloned.Metadata.CustomFields[key] = value
		}
	}

	return &cloned
}

func TestParallelController_ProcessFile_PreservesSecretAcrossChunks(t *testing.T) {
	vectorStore := NewRecordingVectorStore()
	opensearchIndexer := NewMockOpenSearchIndexer()
	embeddingClient := NewMockEmbeddingClient()
	metadataExtractor := NewMockMetadataExtractor()
	metadataExtractor.metadata = &pkgdomain.DocumentMetadata{
		Title:     "Chunked Secret Document",
		Category:  "extracted",
		WordCount: 100,
		Secret:    false,
		CustomFields: map[string]interface{}{
			"secret": false,
		},
	}

	controller := NewParallelController(vectorStore, opensearchIndexer, 1)
	fileInfo := &pkgdomain.FileInfo{
		Path:       "docs/secret-long.md",
		Name:       "secret-long.md",
		Content:    strings.Repeat("a", 9000),
		SourceType: "upload",
		Metadata: pkgdomain.DocumentMetadata{
			Title:  "Original Secret Title",
			Secret: true,
			CustomFields: map[string]interface{}{
				"secret": true,
			},
		},
	}

	result, err := controller.ProcessFiles(
		context.Background(),
		[]*pkgdomain.FileInfo{fileInfo},
		"test-index",
		embeddingClient,
		metadataExtractor,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	storedVectors := vectorStore.StoredVectors()
	require.Greater(t, len(storedVectors), 1)

	assert.True(t, fileInfo.Metadata.Secret)
	assert.Equal(t, true, fileInfo.Metadata.CustomFields["secret"])

	for _, vectorData := range storedVectors {
		assert.True(t, vectorData.Metadata.Secret)
		assert.Equal(t, true, vectorData.Metadata.CustomFields["secret"])
	}

	assert.Equal(t, 1, result.ProcessedFiles)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)
}

func TestParallelController_ProcessFile_DoesNotOverrideSecretForNonUploadSource(t *testing.T) {
	vectorStore := NewRecordingVectorStore()
	opensearchIndexer := NewMockOpenSearchIndexer()
	embeddingClient := NewMockEmbeddingClient()
	metadataExtractor := NewMockMetadataExtractor()
	metadataExtractor.metadata = &pkgdomain.DocumentMetadata{
		Title:     "CLI Front Matter Secret",
		Category:  "extracted",
		WordCount: 50,
		Secret:    true,
		CustomFields: map[string]interface{}{
			"secret": true,
		},
	}

	controller := NewParallelController(vectorStore, opensearchIndexer, 1)
	fileInfo := &pkgdomain.FileInfo{
		Path:    "docs/cli-secret.md",
		Name:    "cli-secret.md",
		Content: "# CLI Secret Document\n\nThis is secret.",
		Metadata: pkgdomain.DocumentMetadata{
			Title:  "CLI Secret Document",
			Secret: false,
		},
	}

	result, err := controller.ProcessFiles(
		context.Background(),
		[]*pkgdomain.FileInfo{fileInfo},
		"test-index",
		embeddingClient,
		metadataExtractor,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, fileInfo.Metadata.Secret, "front-matter secret should not be overwritten by CLI path")
	assert.Equal(t, true, fileInfo.Metadata.CustomFields["secret"])

	assert.Equal(t, 1, result.ProcessedFiles)
	assert.Equal(t, 1, result.SuccessCount)
}
