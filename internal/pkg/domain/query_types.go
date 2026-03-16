package domain

type QueryResult struct {
	Key      string                 `json:"key"`
	Distance float64                `json:"distance"`
	Metadata map[string]interface{} `json:"metadata"`
	Content  string                 `json:"content,omitempty"`
}

type QueryVectorsResult struct {
	Results    []QueryResult `json:"results"`
	TotalCount int           `json:"total_count"`
	Query      string        `json:"query"`
	TopK       int           `json:"top_k"`
}
