package opensearch

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
)

type SearchClient interface {
	SearchTermQuery(ctx context.Context, indexName string, query *TermQuery) (*TermQueryResponse, error)
	SearchBM25(ctx context.Context, indexName string, query *BM25Query) (*BM25SearchResponse, error)
	SearchDenseVector(ctx context.Context, indexName string, query *VectorQuery) (*VectorSearchResponse, error)
	RecordRequest(duration time.Duration, success bool)
	GetMetrics() *PerformanceMetrics
	LogMetrics()
}

type EmbeddingClient interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float64, error)
}

type HybridSearchEngine struct {
	client          SearchClient
	embeddingClient EmbeddingClient
	textProcessor   *JapaneseTextProcessor
	fusionEngine    *FusionEngine
	urlDetector     URLDetector
}

var digitPhrasePattern = regexp.MustCompile(`[0-9０-９]+(?:円|％|%|[\p{Han}\p{Katakana}ーA-Za-z0-9０-９])+`)

type HybridQuery struct {
	Query                  string            `json:"query"`
	IndexName              string            `json:"index_name"`
	Size                   int               `json:"size"`
	From                   int               `json:"from"`
	Fields                 []string          `json:"fields"`
	VectorField            string            `json:"vector_field"`
	K                      int               `json:"k"`
	EfSearch               int               `json:"ef_search,omitempty"`
	Filters                map[string]string `json:"filters,omitempty"`
	MinScore               float64           `json:"min_score,omitempty"`
	BM25Weight             float64           `json:"bm25_weight"`
	VectorWeight           float64           `json:"vector_weight"`
	FusionMethod           FusionMethod      `json:"fusion_method"`
	RankConstant           float64           `json:"rank_constant,omitempty"`
	UseJapaneseNLP         bool              `json:"use_japanese_nlp"`
	TimeoutSeconds         int               `json:"timeout_seconds,omitempty"`
	BM25Operator           string            `json:"bm25_operator,omitempty"`             // "and" or "or", defaults to "or"
	BM25MinimumShouldMatch string            `json:"bm25_minimum_should_match,omitempty"` // e.g., "2", "75%"
}

type HybridSearchResult struct {
	FusionResult   *FusionResult         `json:"fusion_result"`
	BM25Response   *BM25SearchResponse   `json:"bm25_response,omitempty"`
	VectorResponse *VectorSearchResponse `json:"vector_response,omitempty"`
	ProcessedQuery *ProcessedQuery       `json:"processed_query,omitempty"`
	ExecutionTime  time.Duration         `json:"execution_time"`
	BM25Time       time.Duration         `json:"bm25_time"`
	VectorTime     time.Duration         `json:"vector_time"`
	EmbeddingTime  time.Duration         `json:"embedding_time"`
	FusionTime     time.Duration         `json:"fusion_time"`
	Errors         []string              `json:"errors,omitempty"`
	PartialResults bool                  `json:"partial_results"`
	SearchMethod   string                `json:"search_method"`
	URLDetected    bool                  `json:"url_detected"`
	TermQueryTime  time.Duration         `json:"term_query_time,omitempty"`
	FallbackReason string                `json:"fallback_reason,omitempty"`
}

func NewHybridSearchEngine(client SearchClient, embeddingClient EmbeddingClient) *HybridSearchEngine {
	return &HybridSearchEngine{
		client:          client,
		embeddingClient: embeddingClient,
		textProcessor:   NewJapaneseTextProcessor(),
		fusionEngine:    NewFusionEngine(60.0),
		urlDetector:     NewURLDetector(),
	}
}

func (hse *HybridSearchEngine) Search(ctx context.Context, query *HybridQuery) (*HybridSearchResult, error) {
	startTime := time.Now()
	log.Printf("Starting hybrid search: query='%s', index='%s', fusion='%s', weights=BM25:%.2f/Vector:%.2f",
		query.Query, query.IndexName, query.FusionMethod, query.BM25Weight, query.VectorWeight)

	if err := hse.validateQuery(query); err != nil {
		log.Printf("Hybrid search query validation failed: %v", err)
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	if query.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(query.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	result := &HybridSearchResult{
		PartialResults: false,
		SearchMethod:   "hybrid_search",
		URLDetected:    false,
	}

	var processedQuery *ProcessedQuery
	if query.UseJapaneseNLP {
		processedQuery = hse.textProcessor.ProcessQuery(query.Query)
		result.ProcessedQuery = processedQuery
	}

	detector := hse.urlDetector
	if detector == nil {
		detector = NewURLDetector()
		hse.urlDetector = detector
	}

	urlDetected := false
	termQueryDuration := time.Duration(0)
	fallbackReason := ""

	if detector != nil {
		detectionResult := detector.DetectURLs(query.Query)
		if detectionResult != nil && detectionResult.HasURL {
			log.Printf("URL detected in query: %v", detectionResult.URLs)
			urlDetected = true
			termQuery := &TermQuery{
				Field:  "reference.keyword",
				Values: detectionResult.URLs,
				Size:   query.Size,
			}
			if termQuery.Size <= 0 {
				termQuery.Size = len(detectionResult.URLs)
			}

			log.Printf("Executing term query on 'reference.keyword' field for %d URL(s)", len(detectionResult.URLs))
			termStart := time.Now()
			termResponse, err := hse.client.SearchTermQuery(ctx, query.IndexName, termQuery)
			termQueryDuration = time.Since(termStart)

			if err == nil && termResponse != nil && termResponse.TotalHits > 0 {
				log.Printf("Term query found %d result(s) in %v", termResponse.TotalHits, termQueryDuration)
				result.FusionResult = hse.buildExactMatchFusionResult(termResponse)
				result.ExecutionTime = time.Since(startTime)
				result.SearchMethod = "url_exact_match"
				result.URLDetected = true
				result.TermQueryTime = termQueryDuration
				result.FallbackReason = ""
				hse.client.RecordRequest(result.ExecutionTime, true)
				log.Printf("Returning URL exact match with %d result(s) in %v", termResponse.TotalHits, termQueryDuration)
				return result, nil
			}

			if err != nil {
				log.Printf("Term query failed after %v: %v", termQueryDuration, err)
				result.Errors = append(result.Errors, fmt.Sprintf("Term query failed: %v", err))
				fallbackReason = "term_query_error"
			} else {
				log.Printf("Term query returned no results after %v", termQueryDuration)
				fallbackReason = "term_query_no_results"
			}
			if fallbackReason != "" {
				log.Printf("Falling back to hybrid search: %s", fallbackReason)
			}
		} else {
			log.Printf("No URL detected in query")
		}
	}

	return hse.performHybridSearch(ctx, query, processedQuery, result, startTime, urlDetected, termQueryDuration, fallbackReason)
}

func (hse *HybridSearchEngine) performHybridSearch(ctx context.Context, query *HybridQuery, processedQuery *ProcessedQuery, result *HybridSearchResult, startTime time.Time, urlDetected bool, termQueryDuration time.Duration, fallbackReason string) (*HybridSearchResult, error) {
	var bm25Response *BM25SearchResponse
	var vectorResponse *VectorSearchResponse
	var embeddingVector []float64

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		bm25Start := time.Now()
		defer func() {
			result.BM25Time = time.Since(bm25Start)
		}()

		bm25Query := hse.buildBM25Query(query, processedQuery)
		resp, err := hse.client.SearchBM25(gCtx, query.IndexName, bm25Query)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("BM25 search failed: %v", err))
			result.PartialResults = true
			log.Printf("BM25 search failed: %v", err)
			return nil
		}
		bm25Response = resp
		return nil
	})

	g.Go(func() error {
		embeddingStart := time.Now()
		vector, err := hse.embeddingClient.GenerateEmbedding(gCtx, query.Query)
		result.EmbeddingTime = time.Since(embeddingStart)

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Embedding generation failed: %v", err))
			result.PartialResults = true
			log.Printf("Embedding generation failed: %v", err)
			return nil
		}
		embeddingVector = vector

		vectorStart := time.Now()
		defer func() {
			result.VectorTime = time.Since(vectorStart)
		}()

		vectorQuery := hse.buildVectorQuery(query, embeddingVector)
		resp, err := hse.client.SearchDenseVector(gCtx, query.IndexName, vectorQuery)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Vector search failed: %v", err))
			result.PartialResults = true
			log.Printf("Vector search failed: %v", err)
			return nil
		}
		vectorResponse = resp
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("hybrid search failed: %w", err)
	}

	if bm25Response == nil && vectorResponse == nil {
		return nil, fmt.Errorf("both BM25 and vector searches failed")
	}

	fusionStart := time.Now()
	fusionResult, err := hse.fusionEngine.FuseResults(
		bm25Response,
		vectorResponse,
		query.FusionMethod,
		query.BM25Weight,
		query.VectorWeight,
	)
	result.FusionTime = time.Since(fusionStart)

	if err != nil {
		return nil, fmt.Errorf("result fusion failed: %w", err)
	}

	if query.MinScore > 0 {
		fusionResult.Documents = hse.fusionEngine.ApplyThreshold(fusionResult.Documents, query.MinScore)
	}

	if query.Size > 0 && len(fusionResult.Documents) > query.Size {
		fusionResult.Documents = hse.fusionEngine.LimitResults(fusionResult.Documents, query.Size)
		fusionResult.TotalHits = len(fusionResult.Documents)
	}

	result.FusionResult = fusionResult
	result.BM25Response = bm25Response
	result.VectorResponse = vectorResponse
	result.ExecutionTime = time.Since(startTime)
	result.SearchMethod = "hybrid_search"
	result.URLDetected = urlDetected
	result.TermQueryTime = termQueryDuration
	result.FallbackReason = fallbackReason

	hse.client.RecordRequest(result.ExecutionTime, true)

	log.Printf("Search completed: method=%s, results=%d, time=%v", result.SearchMethod, fusionResult.TotalHits, result.ExecutionTime)
	log.Printf("Performance breakdown - BM25: %v, Vector: %v, Embedding: %v, Fusion: %v",
		result.BM25Time, result.VectorTime, result.EmbeddingTime, result.FusionTime)
	log.Printf("Results summary - Total: %d, BM25: %d, Vector: %d, Fusion: %s",
		fusionResult.TotalHits, fusionResult.BM25Results, fusionResult.VectorResults, fusionResult.FusionType)

	if len(result.Errors) > 0 {
		log.Printf("Search completed with %d warnings: %v", len(result.Errors), result.Errors)
	}

	if hse.client.GetMetrics().RequestCount%10 == 0 {
		hse.client.LogMetrics()
	}

	return result, nil
}

func (hse *HybridSearchEngine) buildExactMatchFusionResult(termResponse *TermQueryResponse) *FusionResult {
	if termResponse == nil {
		return nil
	}

	documents := make([]ScoredDoc, len(termResponse.Results))
	var maxScore float64

	for i, hit := range termResponse.Results {
		score := hit.Score
		documents[i] = ScoredDoc{
			ID:         hit.ID,
			Score:      score,
			FusedScore: score,
			Source:     hit.Source,
			Index:      hit.Index,
			Rank:       i + 1,
			SearchType: "url_exact_match",
		}
		if score > maxScore {
			maxScore = score
		}
	}

	return &FusionResult{
		Documents:     documents,
		TotalHits:     termResponse.TotalHits,
		MaxScore:      maxScore,
		BM25Results:   0,
		VectorResults: 0,
		FusionType:    "url_exact_match",
	}
}

func (hse *HybridSearchEngine) validateQuery(query *HybridQuery) error {
	if query == nil {
		return fmt.Errorf("query cannot be nil")
	}

	if query.Query == "" {
		return fmt.Errorf("query string cannot be empty")
	}

	if query.IndexName == "" {
		return fmt.Errorf("index name cannot be empty")
	}

	if query.Size <= 0 {
		query.Size = 10
	}
	if query.Size > 1000 {
		query.Size = 1000
	}

	if query.K <= 0 {
		query.K = 50
	}
	if query.K > 10000 {
		query.K = 10000
	}

	if query.VectorField == "" {
		query.VectorField = "embedding"
	}

	if len(query.Fields) == 0 {
		query.Fields = []string{"title", "content", "body"}
	}

	if query.BM25Weight == 0 && query.VectorWeight == 0 {
		query.BM25Weight = 0.5
		query.VectorWeight = 0.5
	}

	if query.FusionMethod == "" {
		query.FusionMethod = FusionMethodRRF
	}

	if query.RankConstant <= 0 {
		query.RankConstant = 60.0
	}

	if query.TimeoutSeconds <= 0 {
		query.TimeoutSeconds = 30
	}

	return nil
}

func (hse *HybridSearchEngine) buildBM25Query(query *HybridQuery, processedQuery *ProcessedQuery) *BM25Query {
	searchQuery := query.Query
	if processedQuery != nil && processedQuery.Normalized != "" {
		searchQuery = processedQuery.Normalized
	}

	// Use "or" operator by default for better Japanese text matching
	// This allows partial matching which is more suitable for Japanese queries
	operator := "or"
	if query.BM25Operator != "" {
		operator = query.BM25Operator
	}

	minimumShouldMatch := query.BM25MinimumShouldMatch
	if minimumShouldMatch == "" && shouldEnforceExactMatch(searchQuery) {
		minimumShouldMatch = "100%"
		log.Printf("Enforcing minimum_should_match=100%% for digit-heavy query: original=%q normalized=%q", query.Query, searchQuery)
	}

	boostPhrases := extractCriticalPhrases(query.Query)
	if processedQuery != nil && processedQuery.Normalized != "" {
		for _, phrase := range extractCriticalPhrases(processedQuery.Normalized) {
			if !containsString(boostPhrases, phrase) {
				boostPhrases = append(boostPhrases, phrase)
			}
		}
	}
	if len(boostPhrases) > 0 {
		log.Printf("Adding boosted phrase clauses for: %v", boostPhrases)
	}

	return &BM25Query{
		Query:              searchQuery,
		Fields:             query.Fields,
		Operator:           operator,
		MinimumShouldMatch: minimumShouldMatch,
		Filters:            query.Filters,
		Size:               query.K,
		From:               query.From,
		BoostPhrases:       boostPhrases,
	}
}

func (hse *HybridSearchEngine) buildVectorQuery(query *HybridQuery, vector []float64) *VectorQuery {
	return &VectorQuery{
		Vector:      vector,
		VectorField: query.VectorField,
		K:           query.K,
		EfSearch:    query.EfSearch,
		Filters:     query.Filters,
		MinScore:    query.MinScore,
		Size:        query.Size,
		From:        query.From,
	}
}

func shouldEnforceExactMatch(query string) bool {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}

	if utf8.RuneCountInString(trimmed) > 24 {
		return false
	}

	tokens := strings.Fields(trimmed)
	if len(tokens) > 4 {
		return false
	}

	hasDigit := false
	hasCurrency := false

	for _, r := range trimmed {
		if unicode.IsDigit(r) {
			hasDigit = true
			continue
		}
		switch r {
		case '円', '圓', '¥', '￥', '%', '％':
			hasCurrency = true
		}
	}

	return hasDigit && (hasCurrency || len(tokens) <= 2)
}

func extractCriticalPhrases(original string) []string {
	stripped := strings.TrimSpace(original)
	if stripped == "" {
		return nil
	}

	// Normalize by removing spaces so patterns like "0 円" are captured
	cleaned := strings.ReplaceAll(stripped, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "　", "")

	matches := digitPhrasePattern.FindAllString(cleaned, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	phrases := make([]string, 0, len(matches))
	for _, m := range matches {
		candidate := strings.TrimSpace(m)
		if candidate == "" {
			continue
		}

		runeLen := utf8.RuneCountInString(candidate)
		if runeLen < 3 {
			continue
		}
		if runeLen > 16 {
			continue
		}
		if strings.ContainsAny(candidate, ":/.@><") {
			continue
		}

		hasNonDigit := false
		for _, r := range candidate {
			if !unicode.IsDigit(r) {
				hasNonDigit = true
				break
			}
		}
		if !hasNonDigit {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		phrases = append(phrases, candidate)
	}

	return phrases
}

func containsString(slice []string, target string) bool {
	for _, v := range slice {
		if v == target {
			return true
		}
	}
	return false
}

func (hse *HybridSearchEngine) SearchBM25Only(ctx context.Context, query *HybridQuery) (*HybridSearchResult, error) {
	startTime := time.Now()

	if err := hse.validateQuery(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	result := &HybridSearchResult{
		PartialResults: false,
	}

	var processedQuery *ProcessedQuery
	if query.UseJapaneseNLP {
		processedQuery = hse.textProcessor.ProcessQuery(query.Query)
		result.ProcessedQuery = processedQuery
	}

	bm25Start := time.Now()
	bm25Query := hse.buildBM25Query(query, processedQuery)
	bm25Response, err := hse.client.SearchBM25(ctx, query.IndexName, bm25Query)
	result.BM25Time = time.Since(bm25Start)

	if err != nil {
		return nil, fmt.Errorf("BM25 search failed: %w", err)
	}

	fusionStart := time.Now()
	fusionResult, err := hse.fusionEngine.FuseResults(bm25Response, nil, query.FusionMethod, 1.0, 0.0)
	result.FusionTime = time.Since(fusionStart)

	if err != nil {
		return nil, fmt.Errorf("result processing failed: %w", err)
	}

	if query.MinScore > 0 {
		fusionResult.Documents = hse.fusionEngine.ApplyThreshold(fusionResult.Documents, query.MinScore)
	}

	if query.Size > 0 && len(fusionResult.Documents) > query.Size {
		fusionResult.Documents = hse.fusionEngine.LimitResults(fusionResult.Documents, query.Size)
		fusionResult.TotalHits = len(fusionResult.Documents)
	}

	result.FusionResult = fusionResult
	result.BM25Response = bm25Response
	result.ExecutionTime = time.Since(startTime)

	return result, nil
}

func (hse *HybridSearchEngine) SearchVectorOnly(ctx context.Context, query *HybridQuery) (*HybridSearchResult, error) {
	startTime := time.Now()

	if err := hse.validateQuery(query); err != nil {
		return nil, fmt.Errorf("query validation failed: %w", err)
	}

	result := &HybridSearchResult{
		PartialResults: false,
	}

	embeddingStart := time.Now()
	embeddingVector, err := hse.embeddingClient.GenerateEmbedding(ctx, query.Query)
	result.EmbeddingTime = time.Since(embeddingStart)

	if err != nil {
		return nil, fmt.Errorf("embedding generation failed: %w", err)
	}

	vectorStart := time.Now()
	vectorQuery := hse.buildVectorQuery(query, embeddingVector)
	vectorResponse, err := hse.client.SearchDenseVector(ctx, query.IndexName, vectorQuery)
	result.VectorTime = time.Since(vectorStart)

	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	fusionStart := time.Now()
	fusionResult, err := hse.fusionEngine.FuseResults(nil, vectorResponse, query.FusionMethod, 0.0, 1.0)
	result.FusionTime = time.Since(fusionStart)

	if err != nil {
		return nil, fmt.Errorf("result processing failed: %w", err)
	}

	if query.MinScore > 0 {
		fusionResult.Documents = hse.fusionEngine.ApplyThreshold(fusionResult.Documents, query.MinScore)
	}

	if query.Size > 0 && len(fusionResult.Documents) > query.Size {
		fusionResult.Documents = hse.fusionEngine.LimitResults(fusionResult.Documents, query.Size)
		fusionResult.TotalHits = len(fusionResult.Documents)
	}

	result.FusionResult = fusionResult
	result.VectorResponse = vectorResponse
	result.ExecutionTime = time.Since(startTime)

	return result, nil
}
