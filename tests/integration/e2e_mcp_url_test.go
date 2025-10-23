package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mcpStubEmbeddingClient struct {
	vector []float64
}

func (s *mcpStubEmbeddingClient) GenerateEmbedding(context.Context, string) ([]float64, error) {
	return append([]float64(nil), s.vector...), nil
}

type mcpStubSearchClient struct {
	mu             sync.Mutex
	termResponses  map[string]*opensearch.TermQueryResponse
	bm25Response   *opensearch.BM25SearchResponse
	vectorResponse *opensearch.VectorSearchResponse
	metrics        opensearch.PerformanceMetrics
}

func newMCPStubSearchClient() *mcpStubSearchClient {
	return &mcpStubSearchClient{termResponses: make(map[string]*opensearch.TermQueryResponse)}
}

func (s *mcpStubSearchClient) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.termResponses = make(map[string]*opensearch.TermQueryResponse)
	s.bm25Response = nil
	s.vectorResponse = nil
	s.metrics = opensearch.PerformanceMetrics{}
}

func (s *mcpStubSearchClient) SearchTermQuery(ctx context.Context, index string, query *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(query.Values) == 0 {
		return &opensearch.TermQueryResponse{Took: 1}, nil
	}
	key := strings.TrimSpace(query.Values[0])
	if resp, ok := s.termResponses[key]; ok {
		return resp, nil
	}
	return &opensearch.TermQueryResponse{Took: 1, TotalHits: 0}, nil
}

func (s *mcpStubSearchClient) SearchBM25(ctx context.Context, index string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bm25Response == nil {
		s.bm25Response = mcpNewBM25Response("doc-hybrid")
	}
	return s.bm25Response, nil
}

func (s *mcpStubSearchClient) SearchDenseVector(ctx context.Context, index string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vectorResponse == nil {
		s.vectorResponse = mcpNewVectorResponse("doc-hybrid")
	}
	return s.vectorResponse, nil
}

func (s *mcpStubSearchClient) RecordRequest(duration time.Duration, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics.RequestCount++
	if success {
		s.metrics.SuccessCount++
	} else {
		s.metrics.ErrorCount++
	}
	s.metrics.TotalDuration += duration
}

func (s *mcpStubSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := s.metrics
	return &metrics
}

func (s *mcpStubSearchClient) LogMetrics()                       {}
func (s *mcpStubSearchClient) HealthCheck(context.Context) error { return nil }

func TestHybridSearchToolAdapter_URLAwareMode(t *testing.T) {
	stubSearch := newMCPStubSearchClient()
	stubEmbedding := &mcpStubEmbeddingClient{vector: []float64{0.1, 0.2, 0.3}}
	config := &mcpserver.HybridSearchConfig{DefaultIndexName: "docs-index", DefaultSize: 3}
	adapter := mcpserver.NewHybridSearchToolAdapter(stubSearch, stubEmbedding, config, nil)

	tcases := []struct {
		name                string
		query               string
		configure           func()
		expectedMethod      string
		expectedURLDetected bool
	}{
		{
			name:  "url exact match",
			query: "https://example.com/doc のタイトル",
			configure: func() {
				stubSearch.termResponses["https://example.com/doc"] = mcpNewTermResponse("doc-url")
			},
			expectedMethod:      "url_exact_match",
			expectedURLDetected: true,
		},
		{
			name:  "hybrid search fallback",
			query: "機械学習について教えて",
			configure: func() {
				stubSearch.bm25Response = mcpNewBM25Response("doc-hybrid")
				stubSearch.vectorResponse = mcpNewVectorResponse("doc-hybrid")
			},
			expectedMethod:      "hybrid_search",
			expectedURLDetected: false,
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			stubSearch.reset()
			if tc.configure != nil {
				tc.configure()
			}

			params := map[string]interface{}{"query": tc.query, "include_metadata": true}
			result, err := adapter.HandleToolCall(context.Background(), params)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEmpty(t, result.Content)

			var response types.HybridSearchResponse
			require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &response))
			assert.Equal(t, tc.expectedMethod, response.SearchMethod)
			assert.Equal(t, tc.expectedURLDetected, response.URLDetected)
			if tc.expectedMethod == "url_exact_match" {
				assert.Equal(t, 1, response.Total)
			} else {
				assert.Equal(t, "hybrid", response.SearchMode)
			}
		})
	}
}

func mcpNewTermResponse(ids ...string) *opensearch.TermQueryResponse {
	resp := &opensearch.TermQueryResponse{
		Took:      5,
		TotalHits: len(ids),
		Results:   make([]opensearch.TermQueryResult, len(ids)),
	}
	for i, id := range ids {
		payload := map[string]string{
			"title":     "Exact Match Doc",
			"reference": "https://example.com/doc",
			"content":   "content",
		}
		raw, _ := json.Marshal(payload)
		resp.Results[i] = opensearch.TermQueryResult{
			Index:  "docs-index",
			ID:     id,
			Score:  1.0,
			Source: raw,
		}
	}
	return resp
}

func mcpNewBM25Response(ids ...string) *opensearch.BM25SearchResponse {
	resp := &opensearch.BM25SearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.BM25SearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   "Hybrid Doc",
			"content": "bm25",
		}
		raw, _ := json.Marshal(payload)
		resp.Hits.Hits[i] = opensearch.BM25SearchResult{
			Index:  "docs-index",
			ID:     id,
			Score:  float64(len(ids) - i),
			Source: raw,
		}
	}
	return resp
}

func mcpNewVectorResponse(ids ...string) *opensearch.VectorSearchResponse {
	resp := &opensearch.VectorSearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.VectorSearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   "Hybrid Doc",
			"content": "vector",
		}
		raw, _ := json.Marshal(payload)
		resp.Hits.Hits[i] = opensearch.VectorSearchResult{
			Index:  "docs-index",
			ID:     id,
			Score:  float64(len(ids) - i),
			Source: raw,
		}
	}
	return resp
}
