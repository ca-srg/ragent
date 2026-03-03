package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitHubRepos_SingleRepo(t *testing.T) {
	repos, err := ParseGitHubRepos("owner/repo")
	require.NoError(t, err)
	assert.Len(t, repos, 1)
	assert.Equal(t, "owner", repos[0].Owner)
	assert.Equal(t, "repo", repos[0].Name)
}

func TestParseGitHubRepos_MultipleRepos(t *testing.T) {
	repos, err := ParseGitHubRepos("owner1/repo1,owner2/repo2")
	require.NoError(t, err)
	assert.Len(t, repos, 2)
	assert.Equal(t, "owner1", repos[0].Owner)
	assert.Equal(t, "repo1", repos[0].Name)
	assert.Equal(t, "owner2", repos[1].Owner)
	assert.Equal(t, "repo2", repos[1].Name)
}

func TestParseGitHubRepos_WhitespaceHandling(t *testing.T) {
	repos, err := ParseGitHubRepos(" owner/repo , owner2/repo2 ")
	require.NoError(t, err)
	assert.Len(t, repos, 2)
	assert.Equal(t, "owner", repos[0].Owner)
	assert.Equal(t, "repo", repos[0].Name)
	assert.Equal(t, "owner2", repos[1].Owner)
	assert.Equal(t, "repo2", repos[1].Name)
}

func TestParseGitHubRepos_InvalidFormat(t *testing.T) {
	_, err := ParseGitHubRepos("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid GitHub repo format")
}

func TestParseGitHubRepos_EmptyString(t *testing.T) {
	_, err := ParseGitHubRepos("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseGitHubRepos_MissingOwner(t *testing.T) {
	_, err := ParseGitHubRepos("/repo")
	assert.Error(t, err)
}

func TestParseGitHubRepos_MissingRepo(t *testing.T) {
	_, err := ParseGitHubRepos("owner/")
	assert.Error(t, err)
}

func TestParseGitHubRepos_TooManySlashes(t *testing.T) {
	_, err := ParseGitHubRepos("owner/repo/extra")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid GitHub repo format")
}

func TestNewGitHubScanner(t *testing.T) {
	repos := []GitHubRepo{
		{Owner: "owner1", Name: "repo1"},
		{Owner: "owner2", Name: "repo2"},
	}

	s := NewGitHubScanner(repos, "test-token")
	assert.NotNil(t, s)
	assert.Len(t, s.repos, 2)
	assert.Equal(t, "test-token", s.token)
}

func TestNewGitHubScanner_EmptyToken(t *testing.T) {
	repos := []GitHubRepo{{Owner: "owner", Name: "repo"}}
	s := NewGitHubScanner(repos, "")
	assert.NotNil(t, s)
	assert.Empty(t, s.token)
}

func TestGitHubScanner_ScanRepository(t *testing.T) {
	tmpDir := t.TempDir()

	docsDir := filepath.Join(tmpDir, "docs", "guide")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Root readme"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "setup.md"), []byte("# Setup Guide\nContent here"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "data.csv"), []byte("col1,col2\nval1,val2"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o644))

	gitDir := filepath.Join(tmpDir, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]"), 0o644))

	repo := GitHubRepo{Owner: "testowner", Name: "testrepo"}
	s := NewGitHubScanner([]GitHubRepo{repo}, "")

	files, err := s.ScanRepository(context.Background(), repo, tmpDir)
	require.NoError(t, err)

	assert.Len(t, files, 3)

	pathMap := make(map[string]*FileInfo)
	for _, f := range files {
		pathMap[f.Path] = f
	}

	readmeFile, ok := pathMap["github://testowner/testrepo/README.md"]
	require.True(t, ok, "README.md should be found")
	assert.Equal(t, "README.md", readmeFile.Name)
	assert.True(t, readmeFile.IsMarkdown)
	assert.False(t, readmeFile.IsCSV)
	assert.Equal(t, "github", readmeFile.SourceType)
	assert.Equal(t, "# Root readme", readmeFile.Content)
	assert.NotEmpty(t, readmeFile.ContentHash)

	setupFile, ok := pathMap["github://testowner/testrepo/docs/guide/setup.md"]
	require.True(t, ok, "docs/guide/setup.md should be found")
	assert.Equal(t, "setup.md", setupFile.Name)
	assert.True(t, setupFile.IsMarkdown)
	assert.Equal(t, "github", setupFile.SourceType)

	csvFile, ok := pathMap["github://testowner/testrepo/docs/guide/data.csv"]
	require.True(t, ok, "docs/guide/data.csv should be found")
	assert.True(t, csvFile.IsCSV)
	assert.False(t, csvFile.IsMarkdown)

	_, goFileExists := pathMap["github://testowner/testrepo/main.go"]
	assert.False(t, goFileExists, ".go files should not be included")

	for _, f := range files {
		assert.False(t, filepath.IsAbs(f.Path) || len(f.Path) > 200, "paths should use github:// scheme")
	}
}

func TestGitHubScanner_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()

	innerDir, err := os.MkdirTemp(tmpDir, "ragent-test-*")
	require.NoError(t, err)

	s := &GitHubScanner{
		tempDirs: []string{innerDir},
	}

	_, err = os.Stat(innerDir)
	require.NoError(t, err)

	s.Cleanup()

	_, err = os.Stat(innerDir)
	assert.True(t, os.IsNotExist(err))
	assert.Nil(t, s.tempDirs)
}

func TestIsGitHubPath(t *testing.T) {
	assert.True(t, IsGitHubPath("github://owner/repo/file.md"))
	assert.False(t, IsGitHubPath("s3://bucket/file.md"))
	assert.False(t, IsGitHubPath("/local/file.md"))
}

func TestGitHubScanner_FileHelpers(t *testing.T) {
	s := &GitHubScanner{}

	assert.True(t, s.isMarkdownFile("test.md"))
	assert.True(t, s.isMarkdownFile("test.markdown"))
	assert.True(t, s.isMarkdownFile("test.MD"))
	assert.False(t, s.isMarkdownFile("test.txt"))

	assert.True(t, s.isCSVFile("test.csv"))
	assert.True(t, s.isCSVFile("test.CSV"))
	assert.False(t, s.isCSVFile("test.tsv"))

	assert.True(t, s.isSupportedFile("test.md"))
	assert.True(t, s.isSupportedFile("test.csv"))
	assert.False(t, s.isSupportedFile("test.go"))
}
