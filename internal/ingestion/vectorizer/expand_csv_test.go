package vectorizer

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/ingestion/csv"
	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
)

func newTestCSVReaderWithPattern(pattern string) *csv.Reader {
	cfg := &csv.Config{
		CSV: csv.CSVConfig{
			Files: []csv.FileConfig{
				{
					Pattern: pattern,
					Content: csv.ContentConfig{
						Columns: []string{"content"},
					},
					Metadata: csv.MetadataMapping{
						Title: "title",
					},
				},
			},
		},
	}
	return csv.NewReader(cfg)
}

func TestExpandCSVFiles_SkipsFilesWithoutMatchingConfig(t *testing.T) {
	vs := &VectorizerService{csvReader: newTestCSVReaderWithPattern("matched*.csv")}

	files := []*pkgdomain.FileInfo{
		{Path: "matched-foo.csv", IsCSV: true, Content: "title,content\nhello,world\n"},
		{Path: "unmatched-bar.csv", IsCSV: true, Content: "a,b\n1,2\n"},
		{Path: "notes.md", IsCSV: false},
	}

	expanded, err := vs.expandCSVFiles(files)
	require.NoError(t, err, "unmatched CSV must not fail the whole expansion")

	var csvRows, passthrough int
	for _, f := range expanded {
		if f.IsCSV {
			csvRows++
		} else {
			passthrough++
		}
	}
	assert.Equal(t, 1, csvRows, "only matched-foo.csv row should be expanded")
	assert.Equal(t, 1, passthrough, "markdown file should pass through")
}

func TestExpandCSVFiles_PropagatesNonConfigErrors(t *testing.T) {
	vs := &VectorizerService{csvReader: newTestCSVReaderWithPattern("*.csv")}

	// All-empty header row triggers the "invalid CSV header row" error inside
	// readWithConfig, which is NOT ErrNoCSVConfig and therefore must propagate.
	files := []*pkgdomain.FileInfo{
		{Path: "broken.csv", IsCSV: true, Content: ",,,\n1,2,3\n"},
	}

	_, err := vs.expandCSVFiles(files)
	require.Error(t, err)
	assert.False(t, errors.Is(err, csv.ErrNoCSVConfig),
		"non-config errors must still propagate so callers can surface them")
}

func TestExpandCSVFiles_AllUnmatchedStillSucceeds(t *testing.T) {
	vs := &VectorizerService{csvReader: newTestCSVReaderWithPattern("matched*.csv")}

	files := []*pkgdomain.FileInfo{
		{Path: "unmatched-a.csv", IsCSV: true, Content: "a\n1\n"},
		{Path: "unmatched-b.csv", IsCSV: true, Content: "b\n2\n"},
		{Path: "keep.md", IsCSV: false},
	}

	expanded, err := vs.expandCSVFiles(files)
	require.NoError(t, err)
	require.Len(t, expanded, 1)
	assert.Equal(t, "keep.md", expanded[0].Path)
}
