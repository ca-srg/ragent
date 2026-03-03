package opensearch

import "encoding/json"

// TermQuery represents the parameters for executing an OpenSearch term query.
type TermQuery struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
	Size   int      `json:"size,omitempty"`
	From   int      `json:"from,omitempty"`
}

// TermQueryResult captures a single OpenSearch hit returned from a term query.
type TermQueryResult struct {
	Index  string          `json:"_index"`
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

// TermQueryResponse provides a simplified view over the OpenSearch term query response.
type TermQueryResponse struct {
	Took      int               `json:"took"`
	TimedOut  bool              `json:"timed_out"`
	TotalHits int               `json:"total_hits"`
	Results   []TermQueryResult `json:"results"`
}
