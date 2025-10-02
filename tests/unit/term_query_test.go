package unit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

func TestBuildTermQueryBody(t *testing.T) {
	t.Parallel()

	single := &opensearch.TermQuery{
		Field:  "reference",
		Values: []string{"https://example.com"},
		Size:   5,
	}
	singleBody := opensearch.BuildTermQueryBody(single)
	expectedSingle := map[string]interface{}{
		"size": float64(5),
		"from": float64(0),
		"query": map[string]interface{}{
			"terms": map[string]interface{}{
				"reference": []interface{}{"https://example.com"},
			},
		},
	}
	assert.Equal(t, expectedSingle, normalize(singleBody))

	multiple := &opensearch.TermQuery{
		Field:  "reference",
		Values: []string{"https://example.com", "http://test.com"},
		From:   2,
		Size:   10,
	}
	multipleBody := opensearch.BuildTermQueryBody(multiple)
	expectedMultiple := map[string]interface{}{
		"size": float64(10),
		"from": float64(2),
		"query": map[string]interface{}{
			"terms": map[string]interface{}{
				"reference": []interface{}{"https://example.com", "http://test.com"},
			},
		},
	}
	assert.Equal(t, expectedMultiple, normalize(multipleBody))
}

func TestBuildTermQueryResponse(t *testing.T) {
	hits := []opensearchapi.SearchHit{
		{
			Index:  "docs",
			ID:     "1",
			Score:  1.0,
			Source: json.RawMessage(`{"reference":"https://example.com"}`),
		},
		{
			Index:  "docs",
			ID:     "2",
			Score:  0.9,
			Source: json.RawMessage(`{"reference":"http://test.com"}`),
		},
	}

	resp := &opensearchapi.SearchResp{}
	resp.Took = 45
	resp.Timeout = false
	resp.Hits.Total.Value = 2
	resp.Hits.Hits = hits

	result := opensearch.BuildTermQueryResponse(resp)
	if assert.NotNil(t, result) {
		assert.Equal(t, 45, result.Took)
		assert.False(t, result.TimedOut)
		assert.Equal(t, 2, result.TotalHits)
		if assert.Len(t, result.Results, 2) {
			assert.Equal(t, "docs", result.Results[0].Index)
			assert.Equal(t, "1", result.Results[0].ID)
			assert.Equal(t, 1.0, result.Results[0].Score)
			assert.Equal(t, json.RawMessage(`{"reference":"https://example.com"}`), result.Results[0].Source)
		}
	}
}

func normalize(body map[string]interface{}) map[string]interface{} {
	marshaled, _ := json.Marshal(body)
	var normalized map[string]interface{}
	_ = json.Unmarshal(marshaled, &normalized)
	return normalized
}
