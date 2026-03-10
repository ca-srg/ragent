package evalexport

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewEvalRecord(t *testing.T) {
	record := NewEvalRecord("chat", "test query")

	assert.Equal(t, "1.0", record.SchemaVersion)
	assert.Equal(t, "chat", record.Command)
	assert.Equal(t, "test query", record.UserInput)
	assert.NotEmpty(t, record.ID)
	assert.False(t, record.Timestamp.IsZero())
	assert.NotNil(t, record.RetrievedContexts)
	assert.NotNil(t, record.RetrievedDocs)
	assert.NotNil(t, record.References)
	assert.Empty(t, record.RetrievedContexts)
	assert.Empty(t, record.RetrievedDocs)
	assert.Empty(t, record.References)
}

func TestEvalRecordJSON(t *testing.T) {
	original := EvalRecord{
		SchemaVersion:     "1.0",
		ID:                "record-1",
		Timestamp:         time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC),
		Command:           "chat",
		UserInput:         "test query",
		RetrievedContexts: []string{"context-1"},
		Response:          "response",
		RetrievedDocs: []RetrievedDoc{{
			DocID:      "doc1",
			Rank:       1,
			FusedScore: 0.95,
		}},
		RunConfig: RunConfig{
			SearchMode: "hybrid",
			TopK:       5,
		},
		Timing: Timing{
			TotalMs: 100,
			LLMMs:   50,
		},
		References: map[string]string{"doc1": "https://example.com/doc1"},
	}

	data, err := json.Marshal(original)
	assert.NoError(t, err)

	var decoded EvalRecord
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, original.SchemaVersion, decoded.SchemaVersion)
	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Command, decoded.Command)
}

func TestEvalRecordJSONFields(t *testing.T) {
	record := EvalRecord{
		SchemaVersion:     "1.0",
		ID:                "record-1",
		Timestamp:         time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC),
		Command:           "chat",
		UserInput:         "test query",
		RetrievedContexts: []string{},
		Response:          "response",
		RetrievedDocs:     []RetrievedDoc{},
		RunConfig:         RunConfig{},
		Timing:            Timing{},
		References:        map[string]string{},
	}

	data, err := json.Marshal(record)
	assert.NoError(t, err)

	var fields map[string]interface{}
	err = json.Unmarshal(data, &fields)
	assert.NoError(t, err)

	assert.Contains(t, fields, "schema_version")
	assert.Contains(t, fields, "id")
	assert.Contains(t, fields, "timestamp")
	assert.Contains(t, fields, "command")
	assert.Contains(t, fields, "user_input")
	assert.Contains(t, fields, "retrieved_contexts")
	assert.Contains(t, fields, "response")
	assert.Contains(t, fields, "retrieved_docs")
	assert.Contains(t, fields, "run_config")
	assert.Contains(t, fields, "timing")
	assert.Contains(t, fields, "references")
}

func TestRetrievedDocJSON(t *testing.T) {
	original := RetrievedDoc{DocID: "doc1", Rank: 1, FusedScore: 0.95}

	data, err := json.Marshal(original)
	assert.NoError(t, err)

	var decoded RetrievedDoc
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, original.DocID, decoded.DocID)
	assert.Equal(t, original.Rank, decoded.Rank)
	assert.Equal(t, original.FusedScore, decoded.FusedScore)
}

func TestTimingJSON(t *testing.T) {
	original := Timing{TotalMs: 100, LLMMs: 50}

	data, err := json.Marshal(original)
	assert.NoError(t, err)

	var decoded Timing
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, original.TotalMs, decoded.TotalMs)
	assert.Equal(t, original.LLMMs, decoded.LLMMs)
}
