package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractGitHubMetadata_BasicFields(t *testing.T) {
	e := NewMetadataExtractor()
	content := "# Overview\nSome content here"

	meta, err := e.ExtractGitHubMetadata("myorg", "myrepo", "docs/guide/overview.md", content)
	require.NoError(t, err)

	assert.Equal(t, "Overview", meta.Title)
	assert.Equal(t, "guide", meta.Category)
	assert.Equal(t, "myorg", meta.Author)
	assert.Equal(t, "myrepo", meta.Source)
	assert.Equal(t, "https://github.com/myorg/myrepo/blob/main/docs/guide/overview.md", meta.Reference)
	assert.Equal(t, []string{"myorg", "myrepo"}, meta.Tags)
	assert.Equal(t, "github://myorg/myrepo/docs/guide/overview.md", meta.FilePath)
	assert.Greater(t, meta.WordCount, 0)
}

func TestExtractGitHubMetadata_RootLevelFile(t *testing.T) {
	e := NewMetadataExtractor()
	content := "# Root Doc"

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "README.md", content)
	require.NoError(t, err)

	assert.Equal(t, "general", meta.Category)
	assert.Equal(t, "owner", meta.Author)
	assert.Equal(t, "repo", meta.Source)
}

func TestExtractGitHubMetadata_DeepNestedPath(t *testing.T) {
	e := NewMetadataExtractor()
	content := "# Deep Doc"

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "docs/v2/bank/account/overview.md", content)
	require.NoError(t, err)

	assert.Equal(t, "account", meta.Category)
}

func TestExtractGitHubMetadata_FrontmatterOverrides(t *testing.T) {
	e := NewMetadataExtractor()
	content := `---
title: Custom Title
category: custom-cat
author: Custom Author
tags:
  - tag1
  - tag2
---
# Heading
Content`

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "docs/file.md", content)
	require.NoError(t, err)

	assert.Equal(t, "Custom Title", meta.Title)
	assert.Equal(t, "custom-cat", meta.Category)
	assert.Equal(t, "Custom Author", meta.Author)
	assert.Equal(t, []string{"tag1", "tag2"}, meta.Tags)
	assert.Equal(t, "repo", meta.Source)
	assert.Contains(t, meta.Reference, "https://github.com/owner/repo/blob/main/docs/file.md")
}

func TestExtractGitHubMetadata_FrontmatterSourceOverride(t *testing.T) {
	e := NewMetadataExtractor()
	content := `---
source: custom-source
---
# Doc`

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "file.md", content)
	require.NoError(t, err)

	assert.Equal(t, "custom-source", meta.Source)
}

func TestExtractGitHubMetadata_NoHeading(t *testing.T) {
	e := NewMetadataExtractor()
	content := "Just some plain text without heading"

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "notes/readme.md", content)
	require.NoError(t, err)

	assert.Equal(t, "readme", meta.Title)
}

func TestExtractGitHubMetadata_CustomFields(t *testing.T) {
	e := NewMetadataExtractor()
	content := `---
custom_key: custom_value
---
# Doc`

	meta, err := e.ExtractGitHubMetadata("owner", "repo", "file.md", content)
	require.NoError(t, err)

	assert.Equal(t, "custom_value", meta.CustomFields["custom_key"])
}
