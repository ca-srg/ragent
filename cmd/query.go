package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/metrics"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/slacksearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

type QuerySearchClient interface {
	opensearch.SearchClient
	HealthCheck(ctx context.Context) error
}

type appConfigLoader func() (*commontypes.Config, error)
type awsConfigLoader func(ctx context.Context, optFns ...func(*config.LoadOptions) error) (aws.Config, error)
type bedrockClientFactory func(aws.Config, string) opensearch.EmbeddingClient
type openSearchClientFactory func(*opensearch.Config) (QuerySearchClient, error)
type hybridEngineFactory func(opensearch.SearchClient, opensearch.EmbeddingClient) *opensearch.HybridSearchEngine

var (
	queryText      string
	topK           int
	outputJSON     bool
	filterQuery    string
	searchMode     string
	indexName      string
	bm25Weight     float64
	vectorWeight   float64
	fusionMethod   string
	useJapaneseNLP bool
	timeout        int
	queryOnlySlack bool
	slackChannels  []string
)

var (
	loadAppConfig      appConfigLoader      = appconfig.Load
	loadAWSConfig      awsConfigLoader      = config.LoadDefaultConfig
	newEmbeddingClient bedrockClientFactory = func(cfg aws.Config, modelID string) opensearch.EmbeddingClient {
		return bedrock.NewBedrockClient(cfg, modelID)
	}
	newOpenSearchClient openSearchClientFactory = func(cfg *opensearch.Config) (QuerySearchClient, error) {
		return opensearch.NewClient(cfg)
	}
	newHybridEngine hybridEngineFactory = opensearch.NewHybridSearchEngine
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Search using hybrid OpenSearch + S3 Vector",
	Long: `
Search using hybrid OpenSearch BM25 + Dense Vector search with S3 Vector.
Supports two search modes:
- hybrid: OpenSearch BM25 + Dense Vector with result fusion (default)
- opensearch: OpenSearch only (BM25 or Vector)

Examples:
  # Hybrid search (default)
  kiberag query -q "機械学習アルゴリズム"
  
  # OpenSearch only with custom weights
  kiberag query -q "API documentation" --search-mode opensearch --bm25-weight 0.7 --vector-weight 0.3
  
  # Japanese NLP processing
  kiberag query -q "機械学習とデータベース" --japanese-nlp
  
  # Custom fusion method
  kiberag query -q "search algorithms" --fusion-method weighted_sum --top-k 10
`,
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().StringVarP(&queryText, "query", "q", "", "Text query to search for (required)")
	queryCmd.Flags().IntVarP(&topK, "top-k", "k", 10, "Number of similar results to return")
	queryCmd.Flags().BoolVarP(&outputJSON, "json", "j", false, "Output results in JSON format")
	queryCmd.Flags().StringVarP(&filterQuery, "filter", "f", "", "JSON metadata filter (e.g., '{\"category\":\"docs\"}')")

	// New hybrid search flags
	queryCmd.Flags().StringVar(&searchMode, "search-mode", "hybrid", "Search mode: hybrid|opensearch")
	queryCmd.Flags().StringVar(&indexName, "index-name", "", "OpenSearch index name (optional, defaults to config)")
	queryCmd.Flags().Float64Var(&bm25Weight, "bm25-weight", 0.5, "BM25 search weight in hybrid mode (0.0-1.0)")
	queryCmd.Flags().Float64Var(&vectorWeight, "vector-weight", 0.5, "Vector search weight in hybrid mode (0.0-1.0)")
	queryCmd.Flags().StringVar(&fusionMethod, "fusion-method", "rrf", "Result fusion method: rrf|weighted_sum|max_score")
	queryCmd.Flags().BoolVar(&useJapaneseNLP, "japanese-nlp", false, "Enable Japanese text processing and analysis")
	queryCmd.Flags().IntVar(&timeout, "timeout", 30, "Request timeout in seconds")
	queryCmd.Flags().BoolVar(&queryOnlySlack, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")
	queryCmd.Flags().StringSliceVar(&slackChannels, "slack-channels", nil, "Limit Slack search to specific channel names (omit leading #)")

	if err := queryCmd.MarkFlagRequired("query"); err != nil {
		log.Fatalf("Failed to mark query flag as required: %v", err)
	}
}

func runQuery(cmd *cobra.Command, args []string) error {
	metrics.RecordInvocation(metrics.ModeQuery)

	// Handle --only-slack mode
	if queryOnlySlack {
		log.Printf("Starting slack-only search for: %s", queryText)
		return runSlackOnlySearch()
	}

	log.Printf("Starting %s search for: %s", searchMode, queryText)

	// Load configuration
	cfg, err := loadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Set context timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Load AWS configuration
	awsConfig, err := loadAWSConfig(ctx, config.WithRegion(cfg.S3VectorRegion))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create embedding client
	embeddingClient := newEmbeddingClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Execute search based on mode
	switch searchMode {
	case "hybrid":
		return runHybridSearch(ctx, cfg, awsConfig, embeddingClient)
	case "opensearch":
		return runOpenSearchOnly(ctx, cfg, embeddingClient)
	default:
		return fmt.Errorf("invalid search mode: %s. Valid modes: hybrid, opensearch", searchMode)
	}
}

func runSlackOnlySearch() error {
	// Load configuration
	cfg, err := loadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Set context timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Load AWS configuration
	awsConfig, err := loadAWSConfig(ctx, config.WithRegion(cfg.S3VectorRegion))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Execute Slack-only search
	result, err := performSlackOnlySearch(ctx, cfg, awsConfig, queryText, slackChannels, nil)
	if err != nil {
		return fmt.Errorf("slack search failed: %w", err)
	}

	return outputSlackOnlyResults(result, queryText, outputJSON)
}

func runHybridSearch(ctx context.Context, cfg *commontypes.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient) error {
	// Fetch messages from Slack URLs in the query (if any)
	var slackURLMessages []slacksearch.EnrichedMessage
	urlMessages, err := fetchSlackURLContext(ctx, cfg, queryText)
	if err != nil {
		log.Printf("Slack URL fetch warning: %v", err)
	} else if len(urlMessages) > 0 {
		slackURLMessages = urlMessages
		log.Printf("Fetched %d message(s) from Slack URL(s)", len(urlMessages))
	}

	// Run document hybrid search
	docResult, docErr := attemptOpenSearchHybrid(ctx, cfg, embeddingClient)
	if docErr != nil {
		return fmt.Errorf("hybrid search failed: %w", docErr)
	}

	var slackResult *slacksearch.SlackSearchResult
	if cfg.SlackSearchEnabled {
		var err error
		slackResult, err = slackSearchRunner(ctx, cfg, awsCfg, embeddingClient, queryText, slackChannels, nil)
		if err != nil {
			log.Printf("Slack search unavailable: %v", err)
		}
	}

	return outputCombinedResultsWithURLContext(docResult, slackResult, slackURLMessages, "hybrid")
}

func runOpenSearchOnly(ctx context.Context, cfg *commontypes.Config, embeddingClient opensearch.EmbeddingClient) error {
	osResult, err := attemptOpenSearchHybrid(ctx, cfg, embeddingClient)
	if err != nil {
		return fmt.Errorf("OpenSearch search failed: %w", err)
	}
	return outputHybridResults(osResult, "opensearch")
}

func attemptOpenSearchHybrid(ctx context.Context, cfg *commontypes.Config, embeddingClient opensearch.EmbeddingClient) (*opensearch.HybridSearchResult, error) {
	// Validate OpenSearch configuration
	if cfg.OpenSearchEndpoint == "" {
		return nil, fmt.Errorf("OpenSearch endpoint not configured")
	}

	// Create OpenSearch client
	osConfig, err := opensearch.NewConfigFromTypes(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch config: %w", err)
	}

	if err := osConfig.Validate(); err != nil {
		return nil, fmt.Errorf("OpenSearch config validation failed: %w", err)
	}

	osClient, err := newOpenSearchClient(osConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Test connection
	if err := osClient.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("OpenSearch health check failed: %w", err)
	}

	// Create hybrid search engine
	hybridEngine := newHybridEngine(osClient, embeddingClient)

	// Build hybrid query
	hybridQuery := &opensearch.HybridQuery{
		Query:          queryText,
		IndexName:      getIndexName(cfg),
		Size:           topK,
		BM25Weight:     bm25Weight,
		VectorWeight:   vectorWeight,
		FusionMethod:   getFusionMethod(),
		UseJapaneseNLP: useJapaneseNLP,
		TimeoutSeconds: timeout,
	}

	// Parse filters
	if filterQuery != "" {
		filters, err := parseFilters(filterQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filters: %w", err)
		}
		hybridQuery.Filters = filters
	}

	// Execute search
	log.Println("Executing OpenSearch hybrid search...")
	return hybridEngine.Search(ctx, hybridQuery)
}

func getIndexName(cfg *commontypes.Config) string {
	// Use explicitly provided index name if available
	if indexName != "" {
		return indexName
	}

	// Determine index based on search mode
	switch searchMode {
	case "opensearch", "hybrid":
		// Use OpenSearch index for OpenSearch-based searches
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		return "kiberag-documents"
	default:
		// Default: try OpenSearch index first, then S3 Vector index
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		if cfg.AWSS3VectorIndex != "" {
			return cfg.AWSS3VectorIndex
		}
		return "kiberag-documents"
	}
}

func getFusionMethod() opensearch.FusionMethod {
	switch fusionMethod {
	case "weighted_sum":
		return opensearch.FusionMethodWeightedSum
	case "max_score":
		return opensearch.FusionMethodMaxScore
	default:
		return opensearch.FusionMethodRRF
	}
}

func parseFilters(filterJSON string) (map[string]string, error) {
	if filterJSON == "" {
		return nil, nil
	}

	var rawFilters map[string]interface{}
	if err := json.Unmarshal([]byte(filterJSON), &rawFilters); err != nil {
		return nil, err
	}

	filters := make(map[string]string)
	for key, value := range rawFilters {
		if strValue, ok := value.(string); ok {
			filters[key] = strValue
		}
	}

	return filters, nil
}

func outputHybridResults(result *opensearch.HybridSearchResult, searchType string) error {
	return outputCombinedResults(result, nil, searchType)
}

func outputCombinedResults(result *opensearch.HybridSearchResult, slackResult *slacksearch.SlackSearchResult, searchType string) error {
	return outputCombinedResultsWithURLContext(result, slackResult, nil, searchType)
}

func outputCombinedResultsWithURLContext(result *opensearch.HybridSearchResult, slackResult *slacksearch.SlackSearchResult, urlMessages []slacksearch.EnrichedMessage, searchType string) error {
	if outputJSON {
		payload := struct {
			*opensearch.HybridSearchResult
			Query               string                         `json:"query"`
			Mode                string                         `json:"mode"`
			SlackResults        *slacksearch.SlackSearchResult `json:"slack_results,omitempty"`
			ReferencedSlackURLs []slacksearch.EnrichedMessage  `json:"referenced_slack_urls,omitempty"`
		}{
			HybridSearchResult:  result,
			Query:               queryText,
			Mode:                searchType,
			SlackResults:        slackResult,
			ReferencedSlackURLs: urlMessages,
		}
		jsonOutput, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	// Print URL-referenced messages first
	if len(urlMessages) > 0 {
		printSlackURLContext(urlMessages)
	}

	printHybridResults(result, searchType)
	if slackResult != nil {
		printSlackResults(slackResult)
	}

	return nil
}

func printHybridResults(result *opensearch.HybridSearchResult, searchType string) {
	fmt.Printf("\nQuery: %s\n", queryText)
	fmt.Printf("Search Type: %s\n", searchType)
	fmt.Printf("Execution Time: %v\n", result.ExecutionTime)

	if result.ProcessedQuery != nil {
		fmt.Printf("Processed Query Language: %s\n", result.ProcessedQuery.Language)
	}

	if result.FusionResult != nil {
		fmt.Printf("Found %d results (Fusion: %s)\n", result.FusionResult.TotalHits, result.FusionResult.FusionType)
		fmt.Printf("BM25 Results: %d, Vector Results: %d\n", result.FusionResult.BM25Results, result.FusionResult.VectorResults)

		if len(result.Errors) > 0 {
			fmt.Printf("Warnings: %v\n", result.Errors)
			fmt.Printf("Partial Results: %v\n", result.PartialResults)
		}

		fmt.Println("\nResults:")

		if len(result.FusionResult.Documents) == 0 {
			fmt.Println("  (no results found)")
			return
		}

		for i, doc := range result.FusionResult.Documents {
			fmt.Printf("\n  %d. Document: %s\n", i+1, doc.ID)
			fmt.Printf("     Fused Score: %.4f", doc.FusedScore)
			if doc.BM25Score > 0 {
				fmt.Printf(" (BM25: %.4f)", doc.BM25Score)
			}
			if doc.VectorScore > 0 {
				fmt.Printf(" (Vector: %.4f)", doc.VectorScore)
			}
			fmt.Printf(" [%s]\n", doc.SearchType)

			if doc.Source != nil {
				// Unmarshal the source JSON
				var source map[string]interface{}
				if err := json.Unmarshal(doc.Source, &source); err == nil {
					if title, ok := source["title"].(string); ok && title != "" {
						fmt.Printf("     Title: %s\n", title)
					}
					if category, ok := source["category"].(string); ok && category != "" {
						fmt.Printf("     Category: %s\n", category)
					}
					if filePath, ok := source["file_path"].(string); ok && filePath != "" {
						fmt.Printf("     File: %s\n", filePath)
					}
				}
			}
		}
	}

	fmt.Printf("\nPerformance:\n")
	fmt.Printf("  BM25 Time: %v\n", result.BM25Time)
	fmt.Printf("  Vector Time: %v\n", result.VectorTime)
	fmt.Printf("  Embedding Time: %v\n", result.EmbeddingTime)
	fmt.Printf("  Fusion Time: %v\n", result.FusionTime)
}
