package query

import (
	"context"
	"sync"
	"testing"
	"time"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
)

type fakeAttemptOpenSearchClient struct {
	mu          sync.Mutex
	bm25Query   *opensearch.BM25Query
	vectorQuery *opensearch.VectorQuery
}

func (c *fakeAttemptOpenSearchClient) SearchTermQuery(_ context.Context, _ string, query *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
	if query == nil {
		return nil, nil
	}
	return &opensearch.TermQueryResponse{}, nil
}

func (c *fakeAttemptOpenSearchClient) SearchBM25(_ context.Context, _ string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if query != nil {
		filters := map[string]string{}
		for key, value := range query.Filters {
			filters[key] = value
		}
		c.bm25Query = &opensearch.BM25Query{
			ExcludeSecret: query.ExcludeSecret,
			Filters:       filters,
		}
	}
	return &opensearch.BM25SearchResponse{}, nil
}

func (c *fakeAttemptOpenSearchClient) SearchDenseVector(_ context.Context, _ string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if query != nil {
		filters := map[string]string{}
		for key, value := range query.Filters {
			filters[key] = value
		}
		c.vectorQuery = &opensearch.VectorQuery{
			ExcludeSecret: query.ExcludeSecret,
			Filters:       filters,
		}
	}
	return &opensearch.VectorSearchResponse{}, nil
}

func (c *fakeAttemptOpenSearchClient) RecordRequest(_ time.Duration, _ bool) {}

func (c *fakeAttemptOpenSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	return &opensearch.PerformanceMetrics{}
}

func (c *fakeAttemptOpenSearchClient) LogMetrics() {}

func (c *fakeAttemptOpenSearchClient) HealthCheck(_ context.Context) error { return nil }

type fakeAttemptOpenSearchEmbeddingClient struct{}

func (c *fakeAttemptOpenSearchEmbeddingClient) GenerateEmbedding(_ context.Context, _ string) ([]float64, error) {
	return []float64{0.1, 0.2}, nil
}

func TestAttemptOpenSearchHybrid_StripsSecretFilterAndExcludesSecretByDefault(t *testing.T) {
	origNewOpenSearchClient := NewOpenSearchClient
	defer func() {
		NewOpenSearchClient = origNewOpenSearchClient
	}()

	fakeClient := &fakeAttemptOpenSearchClient{}
	NewOpenSearchClient = func(_ *opensearch.Config) (QuerySearchClient, error) {
		return fakeClient, nil
	}

	cfg := &appconfig.Config{
		OpenSearchEndpoint: "http://localhost:9200",
		OpenSearchRegion:   "us-east-1",
		OpenSearchIndex:    "docs-index",
	}

	result, err := attemptOpenSearchHybrid(context.Background(), cfg, &fakeAttemptOpenSearchEmbeddingClient{}, QueryOptions{
		QueryText:      "secret policy test",
		TopK:           10,
		FilterQuery:    `{"category":"docs","secret":"true","SeCrEt":"false"}`,
		SearchMode:     "hybrid",
		UseJapaneseNLP: true,
	})
	if err != nil {
		t.Fatalf("attemptOpenSearchHybrid returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected hybrid search result")
	}

	fakeClient.mu.Lock()
	defer fakeClient.mu.Unlock()

	if fakeClient.bm25Query == nil {
		t.Fatal("expected BM25 query to be built")
	}
	if fakeClient.vectorQuery == nil {
		t.Fatal("expected vector query to be built")
	}

	if !fakeClient.bm25Query.ExcludeSecret {
		t.Error("expected BM25 query to exclude secret by default")
	}
	if !fakeClient.vectorQuery.ExcludeSecret {
		t.Error("expected vector query to exclude secret by default")
	}

	if _, ok := fakeClient.bm25Query.Filters["secret"]; ok {
		t.Errorf("expected BM25 filters to strip secret key")
	}
	if _, ok := fakeClient.vectorQuery.Filters["secret"]; ok {
		t.Errorf("expected vector filters to strip secret key")
	}

	if got := fakeClient.bm25Query.Filters["category"]; got != "docs" {
		t.Errorf("expected category filter to remain, got %q", got)
	}
	if got := fakeClient.vectorQuery.Filters["category"]; got != "docs" {
		t.Errorf("expected category filter to remain, got %q", got)
	}
}
