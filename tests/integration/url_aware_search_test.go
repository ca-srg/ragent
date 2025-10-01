package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSearchClient struct {
	mu         sync.Mutex
	termFn     func(context.Context, string, *opensearch.TermQuery) (*opensearch.TermQueryResponse, error)
	bm25Fn     func(context.Context, string, *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error)
	vectorFn   func(context.Context, string, *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error)
	onTermHook func(*opensearch.TermQuery)

	termCalls   int
	bm25Calls   int
	vectorCalls int
	lastTerm    *opensearch.TermQuery
}

func (m *mockSearchClient) SearchTermQuery(ctx context.Context, index string, query *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
	m.mu.Lock()
	m.termCalls++
	m.lastTerm = query
	hook := m.onTermHook
	fn := m.termFn
	m.mu.Unlock()

	if hook != nil {
		hook(query)
	}
	if fn != nil {
		return fn(ctx, index, query)
	}
	return nil, errors.New("termFn not configured")
}

func (m *mockSearchClient) SearchBM25(ctx context.Context, index string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	m.mu.Lock()
	m.bm25Calls++
	fn := m.bm25Fn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, index, query)
	}
	return nil, errors.New("bm25Fn not configured")
}

func (m *mockSearchClient) SearchDenseVector(ctx context.Context, index string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	m.mu.Lock()
	m.vectorCalls++
	fn := m.vectorFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, index, query)
	}
	return nil, errors.New("vectorFn not configured")
}

func (m *mockSearchClient) RecordRequest(time.Duration, bool) {}

func (m *mockSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	return &opensearch.PerformanceMetrics{}
}

func (m *mockSearchClient) LogMetrics() {}

func (m *mockSearchClient) TermCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.termCalls
}

func (m *mockSearchClient) BM25Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bm25Calls
}

func (m *mockSearchClient) VectorCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vectorCalls
}

func (m *mockSearchClient) LastTermValues() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastTerm == nil {
		return nil
	}
	values := make([]string, len(m.lastTerm.Values))
	copy(values, m.lastTerm.Values)
	return values
}

type mockEmbeddingClient struct {
	vector []float64
}

func (m *mockEmbeddingClient) GenerateEmbedding(context.Context, string) ([]float64, error) {
	return append([]float64(nil), m.vector...), nil
}

func TestURLAwareSearchIntegration(t *testing.T) {
	baseVector := []float64{0.1, 0.2, 0.3}

	testCases := []struct {
		name              string
		query             string
		expectMethod      string
		expectURLDetected bool
		expectFallback    string
		setup             func(t *testing.T, client *mockSearchClient)
		assertFn          func(t *testing.T, client *mockSearchClient, result *opensearch.HybridSearchResult)
	}{
		{
			name:              "exact match returns url method",
			query:             "https://example.com/doc のタイトルを教えて",
			expectMethod:      "url_exact_match",
			expectURLDetected: true,
			expectFallback:    "",
			setup: func(t *testing.T, client *mockSearchClient) {
				client.termFn = func(context.Context, string, *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
					return newTermResponse("doc-1"), nil
				}
				client.bm25Fn = func(context.Context, string, *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
					t.Fatalf("SearchBM25 should not run for exact match path")
					return nil, nil
				}
				client.vectorFn = func(context.Context, string, *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
					t.Fatalf("SearchDenseVector should not run for exact match path")
					return nil, nil
				}
			},
			assertFn: func(t *testing.T, client *mockSearchClient, result *opensearch.HybridSearchResult) {
				require.Equal(t, 1, client.TermCalls())
				assert.Equal(t, 0, client.BM25Calls())
				assert.Equal(t, 0, client.VectorCalls())
				assert.Equal(t, []string{"https://example.com/doc"}, client.LastTermValues())
				require.NotNil(t, result.FusionResult)
				assert.Equal(t, "url_exact_match", result.FusionResult.FusionType)
				assert.Greater(t, result.FusionResult.TotalHits, 0)
			},
		},
		{
			name:              "term query error falls back to hybrid",
			query:             "このURL https://example.com/doc を検索",
			expectMethod:      "hybrid_search",
			expectURLDetected: true,
			expectFallback:    "term_query_error",
			setup: func(t *testing.T, client *mockSearchClient) {
				client.termFn = func(context.Context, string, *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
					return nil, errors.New("opensearch timeout")
				}
				client.bm25Fn = func(context.Context, string, *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
					return newBM25Response("doc-hybrid"), nil
				}
				client.vectorFn = func(context.Context, string, *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
					return newVectorResponse("doc-hybrid"), nil
				}
			},
			assertFn: func(t *testing.T, client *mockSearchClient, result *opensearch.HybridSearchResult) {
				require.Equal(t, 1, client.TermCalls())
				assert.Equal(t, 1, client.BM25Calls())
				assert.Equal(t, 1, client.VectorCalls())
				require.NotNil(t, result.FusionResult)
				assert.Equal(t, string(opensearch.FusionMethodRRF), result.FusionResult.FusionType)
				assert.Greater(t, result.FusionResult.TotalHits, 0)
				assert.Greater(t, result.TermQueryTime, time.Duration(0))
			},
		},
		{
			name:              "no url uses hybrid flow",
			query:             "機械学習について教えて",
			expectMethod:      "hybrid_search",
			expectURLDetected: false,
			expectFallback:    "",
			setup: func(t *testing.T, client *mockSearchClient) {
				client.termFn = func(context.Context, string, *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
					t.Fatalf("SearchTermQuery should not run when no URL is present")
					return nil, nil
				}
				client.bm25Fn = func(context.Context, string, *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
					return newBM25Response("doc-regular"), nil
				}
				client.vectorFn = func(context.Context, string, *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
					return newVectorResponse("doc-regular"), nil
				}
			},
			assertFn: func(t *testing.T, client *mockSearchClient, result *opensearch.HybridSearchResult) {
				assert.Equal(t, 0, client.TermCalls())
				assert.Equal(t, 1, client.BM25Calls())
				assert.Equal(t, 1, client.VectorCalls())
				require.NotNil(t, result.FusionResult)
				assert.Equal(t, string(opensearch.FusionMethodRRF), result.FusionResult.FusionType)
			},
		},
		{
			name:              "multiple urls include all values",
			query:             "https://example.com/doc と http://test.com/page を比較",
			expectMethod:      "url_exact_match",
			expectURLDetected: true,
			expectFallback:    "",
			setup: func(t *testing.T, client *mockSearchClient) {
				client.termFn = func(_ context.Context, _ string, term *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
					return newTermResponse("doc-1", "doc-2"), nil
				}
				client.bm25Fn = func(context.Context, string, *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
					t.Fatalf("SearchBM25 should not run when exact match succeeds")
					return nil, nil
				}
				client.vectorFn = func(context.Context, string, *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
					t.Fatalf("SearchDenseVector should not run when exact match succeeds")
					return nil, nil
				}
			},
			assertFn: func(t *testing.T, client *mockSearchClient, result *opensearch.HybridSearchResult) {
				require.Equal(t, 1, client.TermCalls())
				assert.ElementsMatch(t, []string{"https://example.com/doc", "http://test.com/page"}, client.LastTermValues())
				require.NotNil(t, result.FusionResult)
				assert.Equal(t, "url_exact_match", result.FusionResult.FusionType)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockSearchClient{}
			tc.setup(t, client)

			detectorStart := time.Now()
			client.onTermHook = func(*opensearch.TermQuery) {
				duration := time.Since(detectorStart)
				assert.Less(t, duration, 100*time.Millisecond, "URL detection exceeded 100ms")
			}

			engine := opensearch.NewHybridSearchEngine(client, &mockEmbeddingClient{vector: baseVector})

			result, err := engine.Search(context.Background(), &opensearch.HybridQuery{
				Query:        tc.query,
				IndexName:    "docs",
				Size:         3,
				BM25Weight:   0.6,
				VectorWeight: 0.4,
			})

			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tc.expectMethod, result.SearchMethod)
			assert.Equal(t, tc.expectURLDetected, result.URLDetected)
			assert.Equal(t, tc.expectFallback, result.FallbackReason)
			assert.Less(t, result.TermQueryTime, 200*time.Millisecond, "term query exceeded 200ms")
			assert.Less(t, result.ExecutionTime, 500*time.Millisecond, "search execution too slow")

			if tc.assertFn != nil {
				tc.assertFn(t, client, result)
			}
		})
	}
}

func newTermResponse(ids ...string) *opensearch.TermQueryResponse {
	resp := &opensearch.TermQueryResponse{
		Took:      5,
		TotalHits: len(ids),
		Results:   make([]opensearch.TermQueryResult, len(ids)),
	}
	for i, id := range ids {
		payload := map[string]string{
			"title":     fmt.Sprintf("Doc %d", i+1),
			"reference": fmt.Sprintf("https://example.com/%s", id),
			"content":   "sample content",
		}
		raw, _ := json.Marshal(payload)
		resp.Results[i] = opensearch.TermQueryResult{
			Index:  "docs",
			ID:     id,
			Score:  1.0,
			Source: raw,
		}
	}
	return resp
}

func newBM25Response(ids ...string) *opensearch.BM25SearchResponse {
	resp := &opensearch.BM25SearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.BM25SearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   fmt.Sprintf("BM25 Doc %d", i+1),
			"content": "bm25 content",
		}
		raw, _ := json.Marshal(payload)
		resp.Hits.Hits[i] = opensearch.BM25SearchResult{
			Index:  "docs",
			ID:     id,
			Score:  float64(len(ids) - i),
			Source: raw,
		}
	}
	return resp
}

func newVectorResponse(ids ...string) *opensearch.VectorSearchResponse {
	resp := &opensearch.VectorSearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.VectorSearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   fmt.Sprintf("Vector Doc %d", i+1),
			"content": "vector content",
		}
		raw, _ := json.Marshal(payload)
		resp.Hits.Hits[i] = opensearch.VectorSearchResult{
			Index:  "docs",
			ID:     id,
			Score:  float64(len(ids) - i),
			Source: raw,
		}
	}
	return resp
}
