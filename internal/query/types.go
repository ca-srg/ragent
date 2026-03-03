package query

// QueryResult represents a single result from a vector query
type QueryResult struct {
	Key      string                 `json:"key"`
	Distance float64                `json:"distance"`
	Metadata map[string]interface{} `json:"metadata"`
	Content  string                 `json:"content,omitempty"`
}

// QueryVectorsResult represents the complete result from a vector query
type QueryVectorsResult struct {
	Results    []QueryResult `json:"results"`
	TotalCount int           `json:"total_count"`
	Query      string        `json:"query"`
	TopK       int           `json:"top_k"`
}
