package search_test

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	search "github.com/ca-srg/ragent/internal/pkg/search"
)

var _ *search.HybridSearchService

var _ func(
	*appconfig.Config,
	opensearch.EmbeddingClient,
	*slack.Client,
	*bedrock.BedrockClient,
) (*search.HybridSearchService, error) = search.NewHybridSearchService

func TestSearchRequestZeroValueJSONShape(t *testing.T) {
	t.Parallel()

	var request search.SearchRequest

	payload, err := json.Marshal(request)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"query": "",
		"index_name": "",
		"context_size": 0,
		"bm25_weight": 0,
		"vector_weight": 0,
		"use_japanese_nlp": false,
		"timeout_seconds": 0,
		"enable_slack_search": false
	}`, string(payload))

	assert.Empty(t, request.Query)
	assert.Empty(t, request.IndexName)
	assert.Zero(t, request.ContextSize)
	assert.Zero(t, request.BM25Weight)
	assert.Zero(t, request.VectorWeight)
	assert.False(t, request.UseJapaneseNLP)
	assert.Zero(t, request.TimeoutSeconds)
	assert.Nil(t, request.Filters)
	assert.False(t, request.EnableSlackSearch)
	assert.Nil(t, request.SlackChannels)
}

func TestSearchRequestFieldAssignments(t *testing.T) {
	t.Parallel()

	request := search.SearchRequest{
		Query:             "design doc",
		IndexName:         "ragent-docs",
		ContextSize:       5,
		BM25Weight:        0.7,
		VectorWeight:      0.3,
		UseJapaneseNLP:    true,
		TimeoutSeconds:    15,
		Filters:           map[string]string{"team": "search"},
		EnableSlackSearch: true,
		SlackChannels:     []string{"engineering", "search"},
	}

	payload, err := json.Marshal(request)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"query": "design doc",
		"index_name": "ragent-docs",
		"context_size": 5,
		"bm25_weight": 0.7,
		"vector_weight": 0.3,
		"use_japanese_nlp": true,
		"timeout_seconds": 15,
		"filters": {"team": "search"},
		"enable_slack_search": true,
		"slack_channels": ["engineering", "search"]
	}`, string(payload))

	assert.Equal(t, "design doc", request.Query)
	assert.Equal(t, "ragent-docs", request.IndexName)
	assert.Equal(t, 5, request.ContextSize)
	assert.Equal(t, 0.7, request.BM25Weight)
	assert.Equal(t, 0.3, request.VectorWeight)
	assert.True(t, request.UseJapaneseNLP)
	assert.Equal(t, 15, request.TimeoutSeconds)
	assert.Equal(t, map[string]string{"team": "search"}, request.Filters)
	assert.True(t, request.EnableSlackSearch)
	assert.Equal(t, []string{"engineering", "search"}, request.SlackChannels)
}

func TestSearchResponseJSONShape(t *testing.T) {
	t.Parallel()

	response := search.SearchResponse{
		ContextParts:  []string{"context 1", "context 2"},
		References:    map[string]string{"Design Doc": "https://example.com/design"},
		TotalResults:  2,
		SearchTime:    "12ms",
		IndexUsed:     "ragent-docs",
		SearchMethod:  "hybrid",
		SearchSources: []string{"documents"},
	}

	payload, err := json.Marshal(response)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"context_parts": ["context 1", "context 2"],
		"references": {"Design Doc": "https://example.com/design"},
		"total_results": 2,
		"search_time": "12ms",
		"index_used": "ragent-docs",
		"search_method": "hybrid",
		"search_sources": ["documents"]
	}`, string(payload))
}

func TestHybridSearchServiceTypeExists(t *testing.T) {
	t.Parallel()

	service := &search.HybridSearchService{}
	require.NotNil(t, service)
}
