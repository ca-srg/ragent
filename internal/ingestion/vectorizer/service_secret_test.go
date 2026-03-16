package vectorizer

import (
	"context"
	"testing"

	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessSingleFile_PreservesSecretAfterMetadataExtraction(t *testing.T) {
	metadataExtractor := NewMockMetadataExtractor()
	metadataExtractor.metadata = &pkgdomain.DocumentMetadata{
		Title:     "Extracted Title",
		Category:  "extracted",
		WordCount: 42,
		Secret:    false,
		CustomFields: map[string]interface{}{
			"secret": false,
		},
	}

	service, err := NewVectorizerService(&ServiceConfig{
		Config:            &pkgconfig.Config{},
		EmbeddingClient:   NewMockEmbeddingClient(),
		VectorStoreClient: NewMockVectorStore(),
		MetadataExtractor: metadataExtractor,
		FileScanner:       NewMockFileScanner(),
	})
	require.NoError(t, err)

	fileInfo := &pkgdomain.FileInfo{
		Path:       "docs/secret.md",
		Name:       "secret.md",
		Content:    "top secret content",
		SourceType: "upload",
		Metadata: pkgdomain.DocumentMetadata{
			Title:  "Original Title",
			Secret: true,
			CustomFields: map[string]interface{}{
				"secret": true,
			},
		},
	}

	err = service.ProcessSingleFile(context.Background(), fileInfo, true)
	require.NoError(t, err)

	assert.Equal(t, "Extracted Title", fileInfo.Metadata.Title)
	assert.True(t, fileInfo.Metadata.Secret)
	assert.Equal(t, true, fileInfo.Metadata.CustomFields["secret"])
}

func TestProcessSingleFile_DoesNotOverrideSecretForNonUploadSource(t *testing.T) {
	metadataExtractor := NewMockMetadataExtractor()
	metadataExtractor.metadata = &pkgdomain.DocumentMetadata{
		Title:     "Extracted Title",
		Category:  "extracted",
		WordCount: 42,
		Secret:    true,
		CustomFields: map[string]interface{}{
			"secret": true,
		},
	}

	service, err := NewVectorizerService(&ServiceConfig{
		Config:            &pkgconfig.Config{},
		EmbeddingClient:   NewMockEmbeddingClient(),
		VectorStoreClient: NewMockVectorStore(),
		MetadataExtractor: metadataExtractor,
		FileScanner:       NewMockFileScanner(),
	})
	require.NoError(t, err)

	fileInfo := &pkgdomain.FileInfo{
		Path:    "docs/front-matter-secret.md",
		Name:    "front-matter-secret.md",
		Content: "secret content",
		Metadata: pkgdomain.DocumentMetadata{
			Secret: false,
		},
	}

	err = service.ProcessSingleFile(context.Background(), fileInfo, true)
	require.NoError(t, err)

	assert.Equal(t, "Extracted Title", fileInfo.Metadata.Title)
	assert.True(t, fileInfo.Metadata.Secret)
	assert.Equal(t, true, fileInfo.Metadata.CustomFields["secret"])
}
