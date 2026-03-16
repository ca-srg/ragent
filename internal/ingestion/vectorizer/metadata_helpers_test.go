package vectorizer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
)

func TestApplyUploadSecretMetadata(t *testing.T) {
	tests := []struct {
		name            string
		metadata        *pkgdomain.DocumentMetadata
		secret          bool
		expectNilTarget bool
	}{
		{
			name:     "sets secret true",
			metadata: &pkgdomain.DocumentMetadata{},
			secret:   true,
		},
		{
			name:     "sets secret false",
			metadata: &pkgdomain.DocumentMetadata{},
			secret:   false,
		},
		{
			name:            "nil metadata",
			metadata:        nil,
			secret:          true,
			expectNilTarget: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				applyUploadSecretMetadata(tt.metadata, tt.secret)
			})

			if tt.expectNilTarget {
				assert.Nil(t, tt.metadata)
				return
			}

			assert.Equal(t, tt.secret, tt.metadata.Secret)
			assert.Equal(t, tt.secret, tt.metadata.CustomFields["secret"])
		})
	}
}
