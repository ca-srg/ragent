package contract

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedTermQueryRequest struct {
	Path string
	Body []byte
}

type termQueryRequestBody struct {
	Size  int `json:"size"`
	From  int `json:"from"`
	Query struct {
		Terms map[string][]string `json:"terms"`
	} `json:"query"`
}

func TestSearchTermQueryContract(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-west-2")

	captured := &capturedTermQueryRequest{}

	opensearchResponse := map[string]interface{}{
		"took":      7,
		"timed_out": false,
		"_shards": map[string]int{
			"total":      1,
			"successful": 1,
			"skipped":    0,
			"failed":     0,
		},
		"hits": map[string]interface{}{
			"total": map[string]interface{}{
				"value":    2,
				"relation": "eq",
			},
			"hits": []map[string]interface{}{
				{
					"_index": "docs-index",
					"_id":    "doc-1",
					"_score": 1.5,
					"_source": map[string]interface{}{
						"title":     "Exact Match Doc",
						"reference": "https://example.com/doc",
						"content":   "doc content",
					},
				},
				{
					"_index": "docs-index",
					"_id":    "doc-2",
					"_score": 0.9,
					"_source": map[string]interface{}{
						"title":     "Backup Doc",
						"reference": "http://test.com/page",
						"content":   "backup content",
					},
				},
			},
		},
	}

	var handlerErr error
	var handlerErrMu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			handlerErrMu.Lock()
			handlerErr = err
			handlerErrMu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		captured.Path = r.URL.Path
		captured.Body = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(opensearchResponse); err != nil {
			handlerErrMu.Lock()
			handlerErr = err
			handlerErrMu.Unlock()
		}
	}))
	t.Cleanup(server.Close)

	client, err := opensearch.NewClient(&opensearch.Config{
		Endpoint:          server.URL,
		Region:            "us-west-2",
		InsecureSkipTLS:   true,
		RateLimit:         1000,
		RateBurst:         1000,
		ConnectionTimeout: 5 * time.Second,
		RequestTimeout:    5 * time.Second,
		MaxRetries:        0,
		RetryDelay:        50 * time.Millisecond,
		MaxConnections:    10,
		MaxIdleConns:      10,
		IdleConnTimeout:   30 * time.Second,
	})
	require.NoError(t, err)

	termQuery := &opensearch.TermQuery{
		Field:  "reference",
		Values: []string{" https://example.com/doc ", "http://test.com/page", "https://example.com/doc"},
		Size:   5,
	}

	resp, err := client.SearchTermQuery(context.Background(), "docs-index", termQuery)
	require.NoError(t, err)
	handlerErrMu.Lock()
	defer handlerErrMu.Unlock()
	require.NoError(t, handlerErr)
	require.NotNil(t, resp)

	assert.Equal(t, "/docs-index/_search", captured.Path)

	var requestBody termQueryRequestBody
	require.NoError(t, json.Unmarshal(captured.Body, &requestBody))
	assert.Equal(t, 5, requestBody.Size)
	assert.Equal(t, 0, requestBody.From)

	terms := requestBody.Query.Terms["reference"]
	require.Len(t, terms, 2)
	assert.Equal(t, "https://example.com/doc", terms[0])
	assert.Equal(t, "http://test.com/page", terms[1])

	require.Equal(t, 7, resp.Took)
	assert.False(t, resp.TimedOut)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "doc-1", resp.Results[0].ID)
	assert.Equal(t, "docs-index", resp.Results[0].Index)
	assert.InDelta(t, 1.5, resp.Results[0].Score, 0.0001)

	var source map[string]string
	require.NoError(t, json.Unmarshal(resp.Results[0].Source, &source))
	assert.Equal(t, "Exact Match Doc", source["title"])
	assert.Equal(t, "https://example.com/doc", source["reference"])
}
