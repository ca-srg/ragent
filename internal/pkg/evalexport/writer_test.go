package evalexport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterCreateDirectory(t *testing.T) {
	dirPath := filepath.Join(t.TempDir(), "deep", "nested", "dir")

	w, err := NewWriter(dirPath)
	require.NoError(t, err)
	require.NotNil(t, w)

	info, err := os.Stat(dirPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestWriterWriteSingleRecord(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	require.NoError(t, err)

	record := NewEvalRecord("query", "test query")
	err = w.WriteRecord(record)
	require.NoError(t, err)

	lines := readJSONLLines(t, dir)
	require.Len(t, lines, 1)

	var got EvalRecord
	err = json.Unmarshal([]byte(lines[0]), &got)
	require.NoError(t, err)
	assert.Equal(t, record.ID, got.ID)
	assert.Equal(t, record.Command, got.Command)
	assert.Equal(t, record.UserInput, got.UserInput)
	assert.Equal(t, record.SchemaVersion, got.SchemaVersion)
}

func TestWriterAppendMultipleRecords(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		record := NewEvalRecord("query", fmt.Sprintf("query %d", i))
		err = w.WriteRecord(record)
		require.NoError(t, err)
	}

	lines := readJSONLLines(t, dir)
	require.Len(t, lines, 3)

	for i, line := range lines {
		var got EvalRecord
		err = json.Unmarshal([]byte(line), &got)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("query %d", i), got.UserInput)
		assert.Equal(t, "query", got.Command)
	}
}

func TestWriterInvalidPath(t *testing.T) {
	_, err := NewWriter("/dev/null/impossible")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create directory")
}

func TestWriterJSONLFormat(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	require.NoError(t, err)

	records := []*EvalRecord{
		NewEvalRecord("query", "first query"),
		NewEvalRecord("chat", "second query"),
	}
	records[0].Response = "first response"
	records[0].References["source"] = "doc-1"
	records[1].Response = "second response"
	records[1].RunConfig.TopK = 5

	for _, record := range records {
		err = w.WriteRecord(record)
		require.NoError(t, err)
	}

	lines := readJSONLLines(t, dir)
	require.Len(t, lines, len(records))

	for i, line := range lines {
		var got EvalRecord
		err = json.Unmarshal([]byte(line), &got)
		require.NoError(t, err)
		assert.Equal(t, records[i].ID, got.ID)
		assert.Equal(t, records[i].Command, got.Command)
		assert.Equal(t, records[i].UserInput, got.UserInput)
		assert.Equal(t, records[i].Response, got.Response)
		assert.Equal(t, records[i].RunConfig.TopK, got.RunConfig.TopK)
		assert.Equal(t, records[i].References, got.References)
	}
}

func readJSONLLines(t *testing.T, dir string) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "eval_*.jsonl"))
	require.NoError(t, err)
	require.Len(t, matches, 1)

	data, err := os.ReadFile(matches[0])
	require.NoError(t, err)

	trimmed := strings.TrimSpace(string(data))
	require.NotEmpty(t, trimmed)

	return strings.Split(trimmed, "\n")
}
