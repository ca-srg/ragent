package benchmark

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/opensearch"
)

type benchStubEmbeddingClient struct {
	vector []float64
}

func (s *benchStubEmbeddingClient) GenerateEmbedding(context.Context, string) ([]float64, error) {
	return append([]float64(nil), s.vector...), nil
}

type benchStubSearchClient struct {
	mu             sync.Mutex
	termResponse   *opensearch.TermQueryResponse
	termErr        error
	bm25Response   *opensearch.BM25SearchResponse
	vectorResponse *opensearch.VectorSearchResponse
	metrics        opensearch.PerformanceMetrics
}

func newBenchStubSearchClient(termResp *opensearch.TermQueryResponse, termErr error, bm25Resp *opensearch.BM25SearchResponse, vectorResp *opensearch.VectorSearchResponse) *benchStubSearchClient {
	return &benchStubSearchClient{
		termResponse:   termResp,
		termErr:        termErr,
		bm25Response:   bm25Resp,
		vectorResponse: vectorResp,
	}
}

func (s *benchStubSearchClient) SearchTermQuery(ctx context.Context, index string, query *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.termResponse, s.termErr
}

func (s *benchStubSearchClient) SearchBM25(ctx context.Context, index string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bm25Response, nil
}

func (s *benchStubSearchClient) SearchDenseVector(ctx context.Context, index string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vectorResponse, nil
}

func (s *benchStubSearchClient) RecordRequest(duration time.Duration, success bool) {
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

func (s *benchStubSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := s.metrics
	return &metrics
}

func (s *benchStubSearchClient) LogMetrics()                       {}
func (s *benchStubSearchClient) HealthCheck(context.Context) error { return nil }

func BenchmarkURLDetector(b *testing.B) {
	origOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origOutput)

	detector := opensearch.NewURLDetector()
	query := "https://example.com/doc のタイトルを教えて"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := detector.DetectURLs(query)
		if res == nil || !res.HasURL {
			b.Fatalf("expected URL detection to succeed")
		}
	}
}

func BenchmarkTermQueryExactMatch(b *testing.B) {
	origOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origOutput)

	termResp := benchNewTermResponse("doc-1")
	stubSearch := newBenchStubSearchClient(termResp, nil, benchNewBM25Response("doc-1"), benchNewVectorResponse("doc-1"))
	stubEmbedding := &benchStubEmbeddingClient{vector: []float64{0.1, 0.2, 0.3}}
	engine := opensearch.NewHybridSearchEngine(stubSearch, stubEmbedding)

	ctx := context.Background()
	hybridQuery := &opensearch.HybridQuery{
		Query:     "https://example.com/doc の概要を教えて",
		IndexName: "docs-index",
		Size:      3,
	}

	if result, err := engine.Search(ctx, hybridQuery); err != nil || result.SearchMethod != "url_exact_match" {
		b.Fatalf("expected url_exact_match result: err=%v method=%s", err, result.SearchMethod)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := engine.Search(ctx, hybridQuery)
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
		if res.TermQueryTime > 200*time.Millisecond {
			b.Fatalf("term query exceeded 200ms: %v", res.TermQueryTime)
		}
	}
}

func BenchmarkFallbackOverhead(b *testing.B) {
	origOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origOutput)

	termResp := benchNewTermResponseWithHits(0)
	stubSearch := newBenchStubSearchClient(termResp, nil, benchNewBM25Response("doc-h1"), benchNewVectorResponse("doc-h1"))
	stubEmbedding := &benchStubEmbeddingClient{vector: []float64{0.1, 0.2, 0.3}}
	engine := opensearch.NewHybridSearchEngine(stubSearch, stubEmbedding)

	ctx := context.Background()
	hybridQuery := &opensearch.HybridQuery{
		Query:     "https://example.com/missing-url",
		IndexName: "docs-index",
		Size:      3,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := engine.Search(ctx, hybridQuery)
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
		if res.SearchMethod != "hybrid_search" {
			b.Fatalf("expected fallback hybrid search, got %s", res.SearchMethod)
		}
		overhead := res.ExecutionTime - res.TermQueryTime
		if overhead > 50*time.Millisecond {
			b.Fatalf("fallback overhead exceeded 50ms: %v", overhead)
		}
	}
}

func benchNewTermResponse(ids ...string) *opensearch.TermQueryResponse {
	resp := benchNewTermResponseWithHits(len(ids))
	for i, id := range ids {
		payload := map[string]string{
			"title":     "Doc",
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

func benchNewTermResponseWithHits(hits int) *opensearch.TermQueryResponse {
	resp := &opensearch.TermQueryResponse{
		Took:      3,
		TotalHits: hits,
		Results:   make([]opensearch.TermQueryResult, hits),
	}
	return resp
}

func benchNewBM25Response(ids ...string) *opensearch.BM25SearchResponse {
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

func benchNewVectorResponse(ids ...string) *opensearch.VectorSearchResponse {
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
