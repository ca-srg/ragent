package types

// SearchRequest contains search parameters
type SearchRequest struct {
	Query     string
	TopK      int
	Filters   map[string]string
	UseHybrid bool
}

// SearchResponse is a generic search response
type SearchResponse struct {
	Query   string
	Total   int
	Results []SearchResultItem
}

type SearchResultItem struct {
	Title   string
	Snippet string
	Score   float64
	Source  string
	Link    string
}
