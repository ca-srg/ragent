package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMetadata_SecretFromFrontmatter(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantSecret     bool
		wantInCustom   bool
		customFieldVal interface{}
	}{
		{
			name: "secret true boolean",
			content: `---
title: Secret Doc
secret: true
---
# Secret Doc
Content`,
			wantSecret:   true,
			wantInCustom: false,
		},
		{
			name: "secret false boolean",
			content: `---
title: Public Doc
secret: false
---
# Public Doc
Content`,
			wantSecret:   false,
			wantInCustom: false,
		},
		{
			name: "no secret field",
			content: `---
title: Normal Doc
---
# Normal Doc
Content`,
			wantSecret:   false,
			wantInCustom: false,
		},
		{
			name:       "no frontmatter",
			content:    "# Plain Doc\nContent",
			wantSecret: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewMetadataExtractor()
			meta, err := e.ExtractMetadata("docs/test.md", tt.content)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSecret, meta.Secret)

			_, inCustom := meta.CustomFields["secret"]
			assert.False(t, inCustom, "secret should be a reserved field and not appear in CustomFields")
		})
	}
}

func TestExtractGitHubMetadata_SecretFromFrontmatter(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantSecret bool
	}{
		{
			name: "secret true",
			content: `---
secret: true
---
# GitHub Secret Doc`,
			wantSecret: true,
		},
		{
			name: "secret false",
			content: `---
secret: false
---
# GitHub Public Doc`,
			wantSecret: false,
		},
		{
			name:       "no secret",
			content:    "# GitHub Doc",
			wantSecret: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewMetadataExtractor()
			meta, err := e.ExtractGitHubMetadata("owner", "repo", "docs/file.md", tt.content)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSecret, meta.Secret)

			_, inCustom := meta.CustomFields["secret"]
			assert.False(t, inCustom, "secret should be a reserved field and not appear in CustomFields")
		})
	}
}

func TestExtractSecret_StringValues(t *testing.T) {
	e := NewMetadataExtractor()

	tests := []struct {
		name       string
		content    string
		wantSecret bool
	}{
		{
			name: "string true",
			content: `---
secret: "true"
---
# Doc`,
			wantSecret: true,
		},
		{
			name: "string TRUE",
			content: `---
secret: "TRUE"
---
# Doc`,
			wantSecret: true,
		},
		{
			name: "string false",
			content: `---
secret: "false"
---
# Doc`,
			wantSecret: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := e.ExtractMetadata("test.md", tt.content)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSecret, meta.Secret)
		})
	}
}

func TestIsReservedField_IncludesSecret(t *testing.T) {
	e := NewMetadataExtractor()
	assert.True(t, e.isReservedField("secret"))
	assert.True(t, e.isReservedField("Secret"))
	assert.True(t, e.isReservedField("SECRET"))
}
