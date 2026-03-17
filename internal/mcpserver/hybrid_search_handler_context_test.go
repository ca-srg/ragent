package mcpserver

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeSDKSearchClient struct {
	mu          sync.Mutex
	bm25Query   *opensearch.BM25Query
	vectorQuery *opensearch.VectorQuery
}

func (c *fakeSDKSearchClient) SearchTermQuery(_ context.Context, _ string, _ *opensearch.TermQuery) (*opensearch.TermQueryResponse, error) {
	return &opensearch.TermQueryResponse{}, nil
}

func (c *fakeSDKSearchClient) SearchBM25(_ context.Context, _ string, query *opensearch.BM25Query) (*opensearch.BM25SearchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if query != nil {
		filters := make(map[string]string, len(query.Filters))
		for key, value := range query.Filters {
			filters[key] = value
		}
		c.bm25Query = &opensearch.BM25Query{ExcludeSecret: query.ExcludeSecret, Filters: filters}
	}
	return &opensearch.BM25SearchResponse{}, nil
}

func (c *fakeSDKSearchClient) SearchDenseVector(_ context.Context, _ string, query *opensearch.VectorQuery) (*opensearch.VectorSearchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if query != nil {
		filters := make(map[string]string, len(query.Filters))
		for key, value := range query.Filters {
			filters[key] = value
		}
		c.vectorQuery = &opensearch.VectorQuery{ExcludeSecret: query.ExcludeSecret, Filters: filters}
	}
	return &opensearch.VectorSearchResponse{}, nil
}

func (c *fakeSDKSearchClient) RecordRequest(_ time.Duration, _ bool) {}

func (c *fakeSDKSearchClient) GetMetrics() *opensearch.PerformanceMetrics {
	return &opensearch.PerformanceMetrics{}
}

func (c *fakeSDKSearchClient) LogMetrics() {}

func (c *fakeSDKSearchClient) HealthCheck(_ context.Context) error { return nil }

type fakeSDKEmbeddingClient struct{}

func (c *fakeSDKEmbeddingClient) GenerateEmbedding(_ context.Context, _ string) ([]float64, error) {
	return []float64{0.1, 0.2}, nil
}

func TestHandleSDKToolCall_PropagatesAuthContextToSecretPolicy(t *testing.T) {
	tests := []struct {
		name            string
		ctx             context.Context
		wantSecretAllow bool
	}{
		{
			name: "OIDC user context allows secret documents",
			ctx: context.WithValue(
				context.WithValue(context.Background(), userContextKey, &TokenInfo{Subject: "user-1"}),
				authMethodContextKey,
				string(AuthMethodOIDC),
			),
			wantSecretAllow: true,
		},
		{
			name:            "IP-only context keeps secrets excluded",
			ctx:             context.WithValue(context.Background(), clientIPContextKey, "127.0.0.1"),
			wantSecretAllow: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			searchClient := &fakeSDKSearchClient{}
			embeddingClient := &fakeSDKEmbeddingClient{}
			adapter := NewHybridSearchToolAdapter(searchClient, embeddingClient, &HybridSearchConfig{DefaultIndexName: "ragent-docs"}, nil)
			handler := NewHybridSearchHandlerFromAdapter(adapter)

			ctx := tc.ctx
			if tc.wantSecretAllow {
				ctx = context.WithValue(ctx, clientIPContextKey, "127.0.0.1")
			}

			args, err := json.Marshal(map[string]interface{}{
				"query": "test query",
				"top_k": 1,
			})
			if err != nil {
				t.Fatalf("failed to marshal args: %v", err)
			}

			req := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{
					Name:      "hybrid_search",
					Arguments: args,
				},
			}

			if _, err := handler.HandleSDKToolCall(ctx, req); err != nil {
				t.Fatalf("HandleSDKToolCall returned error: %v", err)
			}

			searchClient.mu.Lock()
			defer searchClient.mu.Unlock()
			if searchClient.bm25Query == nil || searchClient.vectorQuery == nil {
				t.Fatalf("expected BM25 and vector queries to be executed")
			}
			if got := searchClient.bm25Query.ExcludeSecret; got != !tc.wantSecretAllow {
				t.Errorf("unexpected bm25 exclude_secret value: got %v, want %v", got, !tc.wantSecretAllow)
			}
			if got := searchClient.vectorQuery.ExcludeSecret; got != !tc.wantSecretAllow {
				t.Errorf("unexpected vector exclude_secret value: got %v, want %v", got, !tc.wantSecretAllow)
			}
		})
	}
}

func TestGetAuthMethodAndClientIPHelpers(t *testing.T) {
	if got := getAuthMethodFromContext(context.WithValue(context.Background(), authMethodContextKey, string(AuthMethodOIDC))); got != string(AuthMethodOIDC) {
		t.Fatalf("expected string auth method %q, got %q", string(AuthMethodOIDC), got)
	}

	if got := getAuthMethodFromContext(context.WithValue(context.Background(), authMethodContextKey, AuthMethodIP)); got != string(AuthMethodIP) {
		t.Fatalf("expected typed auth method %q, got %q", string(AuthMethodIP), got)
	}

	if got := getClientIPFromContext(context.WithValue(context.Background(), clientIPContextKey, "192.0.2.1")); got != "192.0.2.1" {
		t.Fatalf("expected client IP, got %q", got)
	}

	if got := getClientIPFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty client IP for nil context value, got %q", got)
	}
}
