package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

type BM25SearchResult struct {
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
	Index  string          `json:"_index"`
}

// Shards represents the shard statistics
type Shards struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

type BM25SearchResponse struct {
	Hits struct {
		Total struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		Hits []BM25SearchResult `json:"hits"`
	} `json:"hits"`
	Shards   Shards `json:"_shards"`
	TimedOut bool   `json:"timed_out"`
	Took     int    `json:"took"`
}

type BM25Query struct {
	Query              string            `json:"query"`
	Fields             []string          `json:"fields"`
	K1                 float64           `json:"k1,omitempty"`
	B                  float64           `json:"b,omitempty"`
	Operator           string            `json:"operator,omitempty"`
	MinimumShouldMatch string            `json:"minimum_should_match,omitempty"`
	Filters            map[string]string `json:"filters,omitempty"`
	Size               int               `json:"size,omitempty"`
	From               int               `json:"from,omitempty"`
}

func (c *Client) SearchBM25(ctx context.Context, indexName string, query *BM25Query) (*BM25SearchResponse, error) {
	if query == nil {
		return nil, NewSearchError("validation", "query cannot be nil")
	}

	if query.Query == "" {
		return nil, NewSearchError("validation", "query string cannot be empty")
	}

	// Set defaults
	if query.Size <= 0 {
		query.Size = 10
	}
	if query.Size > 1000 {
		query.Size = 1000
	}
	if len(query.Fields) == 0 {
		query.Fields = []string{"title", "content", "body"}
	}

	startTime := time.Now()
	var result *BM25SearchResponse

	operation := func() error {
		if err := c.WaitForRateLimit(ctx); err != nil {
			return fmt.Errorf("rate limit error: %w", err)
		}

		searchBody := c.buildBM25SearchBody(query)
		bodyJSON, err := json.Marshal(searchBody)
		if err != nil {
			return NewSearchError("validation", fmt.Sprintf("failed to marshal search body: %v", err))
		}

		req := &opensearchapi.SearchReq{
			Indices: []string{indexName},
			Body:    strings.NewReader(string(bodyJSON)),
		}

		searchResp, err := c.client.Search(ctx, req)
		if err != nil {
			return ClassifyConnectionError(err)
		}

		// Parse the response from opensearch-go v4
		if searchResp == nil {
			return NewSearchError("response", "received nil response from OpenSearch")
		}

		// Convert opensearchapi.SearchResp to our BM25SearchResponse
		bm25Response := &BM25SearchResponse{
			Took:   searchResp.Took,
			Shards: Shards{
				Total:      searchResp.Shards.Total,
				Successful: searchResp.Shards.Successful,
				Skipped:    searchResp.Shards.Skipped,
				Failed:     searchResp.Shards.Failed,
			},
		}

		// Set the total hits
		bm25Response.Hits.Total.Value = searchResp.Hits.Total.Value
		bm25Response.Hits.Total.Relation = searchResp.Hits.Total.Relation

		// Convert each hit
		bm25Response.Hits.Hits = make([]BM25SearchResult, len(searchResp.Hits.Hits))
		for i, hit := range searchResp.Hits.Hits {
			bm25Response.Hits.Hits[i] = BM25SearchResult{
				Index:  hit.Index,
				ID:     hit.ID,
				Score:  float64(hit.Score),
				Source: hit.Source, // json.RawMessage, can be unmarshaled later
			}
		}

		result = bm25Response
		return nil
	}

	err := c.ExecuteWithRetry(ctx, operation, "BM25Search")

	// Record metrics
	duration := time.Since(startTime)
	c.RecordRequest(duration, err == nil)

	if err == nil && result != nil {
		log.Printf("BM25 search completed in %v, found %d results",
			duration, result.Hits.Total.Value)
	}

	return result, err
}

func (c *Client) buildBM25SearchBody(query *BM25Query) map[string]interface{} {
	body := map[string]interface{}{
		"size": query.Size,
		"from": query.From,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []map[string]interface{}{
					c.buildMatchQuery(query),
				},
			},
		},
		"sort": []map[string]interface{}{
			{"_score": map[string]string{"order": "desc"}},
		},
	}

	if len(query.Filters) > 0 {
		filters := make([]map[string]interface{}, 0, len(query.Filters))
		for field, value := range query.Filters {
			filters = append(filters, map[string]interface{}{
				"term": map[string]string{
					field: value,
				},
			})
		}
		body["query"].(map[string]interface{})["bool"].(map[string]interface{})["filter"] = filters
	}

	return body
}

func (c *Client) buildMatchQuery(query *BM25Query) map[string]interface{} {
	if len(query.Fields) == 1 {
		matchQuery := map[string]interface{}{
			"match": map[string]interface{}{
				query.Fields[0]: map[string]interface{}{
					"query": query.Query,
				},
			},
		}

		if query.Operator != "" {
			matchQuery["match"].(map[string]interface{})[query.Fields[0]].(map[string]interface{})["operator"] = query.Operator
		}

		if query.MinimumShouldMatch != "" {
			matchQuery["match"].(map[string]interface{})[query.Fields[0]].(map[string]interface{})["minimum_should_match"] = query.MinimumShouldMatch
		}

		if query.K1 > 0 || query.B > 0 {
			similarity := make(map[string]interface{})
			if query.K1 > 0 {
				similarity["k1"] = query.K1
			}
			if query.B > 0 {
				similarity["b"] = query.B
			}
		}

		return matchQuery
	}

	multiMatchQuery := map[string]interface{}{
		"multi_match": map[string]interface{}{
			"query":  query.Query,
			"fields": query.Fields,
			"type":   "best_fields",
		},
	}

	if query.Operator != "" {
		multiMatchQuery["multi_match"].(map[string]interface{})["operator"] = query.Operator
	}

	if query.MinimumShouldMatch != "" {
		multiMatchQuery["multi_match"].(map[string]interface{})["minimum_should_match"] = query.MinimumShouldMatch
	}

	return multiMatchQuery
}

func (c *Client) CreateBM25Index(ctx context.Context, indexName string, k1, b float64) error {
	if err := c.WaitForRateLimit(ctx); err != nil {
		return fmt.Errorf("rate limit error: %w", err)
	}

	settings := map[string]interface{}{
		"settings": map[string]interface{}{
			"index": map[string]interface{}{
				"similarity": map[string]interface{}{
					"bm25_custom": map[string]interface{}{
						"type": "BM25",
						"k1":   k1,
						"b":    b,
					},
				},
			},
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":       "text",
					"similarity": "bm25_custom",
					"analyzer":   "kuromoji",
				},
				"content": map[string]interface{}{
					"type":       "text",
					"similarity": "bm25_custom",
					"analyzer":   "kuromoji",
				},
				"body": map[string]interface{}{
					"type":       "text",
					"similarity": "bm25_custom",
					"analyzer":   "kuromoji",
				},
				"category": map[string]interface{}{
					"type": "keyword",
				},
				"tags": map[string]interface{}{
					"type": "keyword",
				},
				"created_at": map[string]interface{}{
					"type": "date",
				},
				"updated_at": map[string]interface{}{
					"type": "date",
				},
			},
		},
	}

	bodyJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal index settings: %w", err)
	}

	req := opensearchapi.IndicesCreateReq{
		Index: indexName,
		Body:  strings.NewReader(string(bodyJSON)),
	}

	_, err = c.client.Indices.Create(ctx, req)
	if err != nil {
		return ClassifyConnectionError(err)
	}

	return nil
}
