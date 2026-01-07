package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/ca-srg/ragent/cmd"
	"github.com/ca-srg/ragent/internal/opensearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubEmbeddingClient struct {
	vector []float64
}

func (s *stubEmbeddingClient) GenerateEmbedding(context.Context, string) ([]float64, error) {
	return append([]float64(nil), s.vector...), nil
}

type stubSearchClient struct {
	mu             sync.Mutex
	termResponses  map[string]*opensearch.TermQueryResponse
	bm25Response   *opensearch.BM25SearchResponse
	vectorResponse *opensearch.VectorSearchResponse
	metrics        opensearch.PerformanceMetrics
}

func newStubSearchClient() *stubSearchClient {
	return &stubSearchClient{
		termResponses: make(map[string]*opensearch.TermQueryResponse),
	}
}

func (s *stubSearchClient) SearchTermQuery(ctx context.Context, index string, query *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
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

func (s *stubSearchClient) SearchBM25(ctx context.Context, index string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bm25Response == nil {
		s.bm25Response = newBM25ResponseStub("doc-hybrid")
	}
	return s.bm25Response, nil
}

func (s *stubSearchClient) SearchDenseVector(ctx context.Context, index string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vectorResponse == nil {
		s.vectorResponse = newVectorResponseStub("doc-hybrid")
	}
	return s.vectorResponse, nil
}

func (s *stubSearchClient) RecordRequest(duration time.Duration, success bool) {
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

func (s *stubSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := s.metrics
	return &metrics
}

func (s *stubSearchClient) LogMetrics() {}

func (s *stubSearchClient) HealthCheck(context.Context) error { return nil }

func TestQueryCommandURLAwareSearch(t *testing.T) {
	tcases := []struct {
		name                string
		query               string
		configureStub       func(client *stubSearchClient)
		expectedMethod      string
		expectedURLDetected bool
	}{
		{
			name:  "url exact match path",
			query: "https://example.com/doc の概要",
			configureStub: func(client *stubSearchClient) {
				client.termResponses["https://example.com/doc"] = newTermResponseStub("doc-url")
			},
			expectedMethod:      "url_exact_match",
			expectedURLDetected: true,
		},
		{
			name:  "url exact match with angle brackets",
			query: "Kibela にある <https://example.com/doc> の内容を教えて",
			configureStub: func(client *stubSearchClient) {
				client.termResponses["https://example.com/doc"] = newTermResponseStub("doc-url-bracket")
			},
			expectedMethod:      "url_exact_match",
			expectedURLDetected: true,
		},
		{
			name:  "hybrid fallback without url",
			query: "機械学習について教えて",
			configureStub: func(client *stubSearchClient) {
				client.bm25Response = newBM25ResponseStub("doc-hybrid")
				client.vectorResponse = newVectorResponseStub("doc-hybrid")
			},
			expectedMethod:      "hybrid_search",
			expectedURLDetected: false,
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			stubSearch := newStubSearchClient()
			stubEmbedding := &stubEmbeddingClient{vector: []float64{0.1, 0.2, 0.3}}
			if tc.configureStub != nil {
				tc.configureStub(stubSearch)
			}

			restore := cmd.OverrideQueryDependencies(cmd.QueryDependencyOverrides{
				LoadConfig: cmd.DefaultLoadConfigOverride(&commontypes.Config{
					OpenSearchEndpoint: "http://localhost:9200",
					OpenSearchRegion:   "us-west-2",
					OpenSearchIndex:    "docs-index",
					S3VectorRegion:     "us-west-2",
				}, nil),
				LoadAWSConfig: cmd.DefaultAWSConfigOverride(aws.Config{Region: "us-west-2"}, nil),
				NewEmbeddingClient: func(cfg aws.Config, modelID string) opensearch.EmbeddingClient {
					return stubEmbedding
				},
				NewOpenSearchClient: func(cfg *opensearch.Config) (cmd.QuerySearchClient, error) {
					return stubSearch, nil
				},
			})
			defer restore()

			cmd.ResetQueryState()

			output, err := captureCommandOutput(func() error {
				os.Args = []string{"ragent", "query", "-q", tc.query, "--json"}
				return cmd.Execute()
			})
			require.NoError(t, err)

			var result opensearch.HybridSearchResult
			require.NoError(t, json.Unmarshal([]byte(output), &result), "output should be valid JSON")
			assert.Equal(t, tc.expectedMethod, result.SearchMethod)
			assert.Equal(t, tc.expectedURLDetected, result.URLDetected)
		})
	}
}

func captureCommandOutput(run func() error) (string, error) {
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run()
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		return "", copyErr
	}

	return strings.TrimSpace(buf.String()), err
}

func newTermResponseStub(ids ...string) *opensearch.TermQueryResponse {
	resp := &opensearch.TermQueryResponse{
		Took:      5,
		TotalHits: len(ids),
		Results:   make([]opensearch.TermQueryResult, len(ids)),
	}
	for i, id := range ids {
		payload := map[string]string{
			"title":     "Exact Doc",
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

func newBM25ResponseStub(ids ...string) *opensearch.BM25SearchResponse {
	resp := &opensearch.BM25SearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.BM25SearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   "BM25 Doc",
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

func newVectorResponseStub(ids ...string) *opensearch.VectorSearchResponse {
	resp := &opensearch.VectorSearchResponse{}
	resp.Hits.Total.Value = len(ids)
	resp.Hits.Total.Relation = "eq"
	resp.Hits.Hits = make([]opensearch.VectorSearchResult, len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":   "Vector Doc",
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
