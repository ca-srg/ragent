package vectorizer

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
)

func newTestVectorData(secret bool) *pkgdomain.VectorData {
	now := time.Now()

	return &pkgdomain.VectorData{
		ID:        "test-id",
		Embedding: make([]float64, 1536),
		Metadata: pkgdomain.DocumentMetadata{
			Title:     "Test Title",
			Secret:    secret,
			Tags:      []string{},
			CreatedAt: now,
			UpdatedAt: now,
		},
		Content:   "test content",
		CreatedAt: now,
	}
}

func TestOpenSearchDocument_ToMap_IncludesSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret bool
	}{
		{
			name:   "secret true",
			secret: true,
		},
		{
			name:   "secret false",
			secret: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := NewOpenSearchDocument(newTestVectorData(tt.secret), "")

			m := doc.ToMap()

			assert.Equal(t, tt.secret, m["secret"])
		})
	}
}

func TestOpenSearchDocument_MarshalJSON_IncludesSecret(t *testing.T) {
	doc := NewOpenSearchDocument(newTestVectorData(true), "")

	data, err := doc.MarshalJSON()
	assert.NoError(t, err)

	var actual map[string]interface{}
	err = json.Unmarshal(data, &actual)
	assert.NoError(t, err)
	assert.Equal(t, true, actual["secret"])
}

func TestOpenSearchDocument_Clone_PreservesSecret(t *testing.T) {
	doc := NewOpenSearchDocument(newTestVectorData(true), "")

	clone := doc.Clone()
	if assert.NotNil(t, clone) {
		assert.True(t, clone.Secret)
	}
}
