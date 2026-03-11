package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/slack-go/slack"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/evalexport"
	"github.com/ca-srg/ragent/internal/pkg/metrics"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

// QuerySearchClient extends SearchClient with a HealthCheck method.
type QuerySearchClient interface {
	opensearch.SearchClient
	HealthCheck(ctx context.Context) error
}

// Dependency injection types.
type AppConfigLoader func() (*appconfig.Config, error)
type AWSConfigLoader func(ctx context.Context, optFns ...func(*awssdkconfig.LoadOptions) error) (aws.Config, error)
type BedrockAWSConfigBuilder func(ctx context.Context, region, bearerToken string) (aws.Config, error)
type OpenSearchClientFactory func(*opensearch.Config) (QuerySearchClient, error)
type HybridEngineFactory func(opensearch.SearchClient, opensearch.EmbeddingClient) *opensearch.HybridSearchEngine

// SlackSearchFn is the function signature for Slack search.
type SlackSearchFn func(
	ctx context.Context,
	cfg *appconfig.Config,
	awsCfg aws.Config,
	embeddingClient opensearch.EmbeddingClient,
	userQuery string,
	channels []string,
	progressHandler func(int, int),
) (*slacksearch.SlackSearchResult, error)

// SlackOnlySearchFn is the function signature for Slack-only search.
type SlackOnlySearchFn func(
	ctx context.Context,
	cfg *appconfig.Config,
	awsCfg aws.Config,
	userQuery string,
	channels []string,
	progressHandler func(int, int),
) (*slacksearch.SlackSearchResult, error)

// FetchSlackURLContextFn is the function signature for fetching Slack URL context.
type FetchSlackURLContextFn func(
	ctx context.Context,
	cfg *appconfig.Config,
	userQuery string,
) ([]slacksearch.EnrichedMessage, error)

// Injectable variables — swappable for tests.
var (
	LoadAppConfig        AppConfigLoader         = appconfig.Load
	LoadAWSConfig        AWSConfigLoader         = awssdkconfig.LoadDefaultConfig
	LoadBedrockAWSConfig BedrockAWSConfigBuilder = bedrock.BuildBedrockAWSConfig
	NewOpenSearchClient  OpenSearchClientFactory = func(cfg *opensearch.Config) (QuerySearchClient, error) {
		return opensearch.NewClient(cfg)
	}
	NewHybridEngine HybridEngineFactory = opensearch.NewHybridSearchEngine

	// Slack injectable operations.
	SlackSearchRunner      SlackSearchFn          = defaultSlackSearch
	PerformSlackOnlySearch SlackOnlySearchFn      = defaultSlackOnlySearch
	FetchSlackURLContext   FetchSlackURLContextFn = defaultFetchSlackURLContext
)

// QueryOptions holds all flag values from cmd/query.go.
type QueryOptions struct {
	QueryText      string
	TopK           int
	OutputJSON     bool
	FilterQuery    string
	SearchMode     string
	IndexName      string
	BM25Weight     float64
	VectorWeight   float64
	FusionMethod   string
	UseJapaneseNLP bool
	Timeout        int
	OnlySlack      bool
	SlackChannels  []string
	ExportEval     bool
	ExportEvalPath string
}

// RunQuery is the exported entry point called from cmd/query.go.
func RunQuery(cmd *cobra.Command, opts QueryOptions) error {
	metrics.RecordInvocation(metrics.ModeQuery)

	if opts.OnlySlack {
		log.Printf("Starting slack-only search for: %s", opts.QueryText)
		return runSlackOnlySearch(opts)
	}

	log.Printf("Starting %s search for: %s", opts.SearchMode, opts.QueryText)

	cfg, err := LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout)*time.Second)
	defer cancel()

	bedrockConfig, err := LoadBedrockAWSConfig(ctx, cfg.BedrockRegion, cfg.BedrockBearerToken)
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	embeddingClient, err := embedding.NewEmbeddingClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create embedding client: %w", err)
	}

	switch opts.SearchMode {
	case "hybrid":
		return runHybridSearch(ctx, cfg, bedrockConfig, embeddingClient, opts)
	case "opensearch":
		return runOpenSearchOnly(ctx, cfg, embeddingClient, opts)
	default:
		return fmt.Errorf("invalid search mode: %s. Valid modes: hybrid, opensearch", opts.SearchMode)
	}
}

func runSlackOnlySearch(opts QueryOptions) error {
	cfg, err := LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout)*time.Second)
	defer cancel()

	bedrockConfig, err := LoadBedrockAWSConfig(ctx, cfg.BedrockRegion, cfg.BedrockBearerToken)
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	result, err := PerformSlackOnlySearch(ctx, cfg, bedrockConfig, opts.QueryText, opts.SlackChannels, nil)
	if err != nil {
		return fmt.Errorf("slack search failed: %w", err)
	}

	if opts.ExportEval {
		record := evalexport.NewEvalRecord("query", opts.QueryText)
		record.RunConfig = evalexport.RunConfig{
			SearchMode:         "slack_only",
			ChatModel:          cfg.ChatModel,
			EmbeddingModel:     "amazon.titan-embed-text-v2:0",
			SlackSearchEnabled: true,
		}
		record.References = map[string]string{}
		if writer, werr := evalexport.NewWriter(opts.ExportEvalPath); werr != nil {
			log.Printf("Warning: failed to create eval export writer: %v", werr)
		} else if werr := writer.WriteRecord(record); werr != nil {
			log.Printf("Warning: failed to export eval record: %v", werr)
		}
	}

	return outputSlackOnlyResults(result, opts.QueryText, opts.OutputJSON)
}

func runHybridSearch(ctx context.Context, cfg *appconfig.Config, awsCfg aws.Config, embeddingClient opensearch.EmbeddingClient, opts QueryOptions) error {
	var slackURLMessages []slacksearch.EnrichedMessage
	urlMessages, err := FetchSlackURLContext(ctx, cfg, opts.QueryText)
	if err != nil {
		log.Printf("Slack URL fetch warning: %v", err)
	} else if len(urlMessages) > 0 {
		slackURLMessages = urlMessages
		log.Printf("Fetched %d message(s) from Slack URL(s)", len(urlMessages))
	}

	docResult, docErr := attemptOpenSearchHybrid(ctx, cfg, embeddingClient, opts)
	if docErr != nil {
		return fmt.Errorf("hybrid search failed: %w", docErr)
	}

	if opts.ExportEval {
		record := buildEvalRecordFromHybridResult(opts, cfg, docResult)
		if writer, err := evalexport.NewWriter(opts.ExportEvalPath); err != nil {
			log.Printf("Warning: failed to create eval export writer: %v", err)
		} else if err := writer.WriteRecord(record); err != nil {
			log.Printf("Warning: failed to export eval record: %v", err)
		}
	}

	var slackResult *slacksearch.SlackSearchResult
	if cfg.SlackSearchEnabled {
		var err error
		slackResult, err = SlackSearchRunner(ctx, cfg, awsCfg, embeddingClient, opts.QueryText, opts.SlackChannels, nil)
		if err != nil {
			log.Printf("Slack search unavailable: %v", err)
		}
	}

	return outputCombinedResultsWithURLContext(docResult, slackResult, slackURLMessages, "hybrid", opts)
}

func runOpenSearchOnly(ctx context.Context, cfg *appconfig.Config, embeddingClient opensearch.EmbeddingClient, opts QueryOptions) error {
	osResult, err := attemptOpenSearchHybrid(ctx, cfg, embeddingClient, opts)
	if err != nil {
		return fmt.Errorf("OpenSearch search failed: %w", err)
	}

	if opts.ExportEval {
		record := buildEvalRecordFromHybridResult(opts, cfg, osResult)
		if writer, werr := evalexport.NewWriter(opts.ExportEvalPath); werr != nil {
			log.Printf("Warning: failed to create eval export writer: %v", werr)
		} else if werr := writer.WriteRecord(record); werr != nil {
			log.Printf("Warning: failed to export eval record: %v", werr)
		}
	}

	return OutputHybridResults(osResult, "opensearch", opts)
}

func attemptOpenSearchHybrid(ctx context.Context, cfg *appconfig.Config, embeddingClient opensearch.EmbeddingClient, opts QueryOptions) (*opensearch.HybridSearchResult, error) {
	if cfg.OpenSearchEndpoint == "" {
		return nil, fmt.Errorf("OpenSearch endpoint not configured")
	}

	osConfig, err := opensearch.NewConfigFromTypes(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch config: %w", err)
	}

	if err := osConfig.Validate(); err != nil {
		return nil, fmt.Errorf("OpenSearch config validation failed: %w", err)
	}

	osClient, err := NewOpenSearchClient(osConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	if err := osClient.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("OpenSearch health check failed: %w", err)
	}

	hybridEngine := NewHybridEngine(osClient, embeddingClient)

	hybridQuery := &opensearch.HybridQuery{
		Query:          opts.QueryText,
		IndexName:      getIndexName(cfg, opts),
		Size:           opts.TopK,
		BM25Weight:     opts.BM25Weight,
		VectorWeight:   opts.VectorWeight,
		FusionMethod:   getFusionMethod(opts),
		UseJapaneseNLP: opts.UseJapaneseNLP,
		TimeoutSeconds: opts.Timeout,
	}

	if opts.FilterQuery != "" {
		filters, err := ParseFilters(opts.FilterQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to parse filters: %w", err)
		}
		hybridQuery.Filters = filters
	}

	log.Println("Executing OpenSearch hybrid search...")
	return hybridEngine.Search(ctx, hybridQuery)
}

func getIndexName(cfg *appconfig.Config, opts QueryOptions) string {
	if opts.IndexName != "" {
		return opts.IndexName
	}
	switch opts.SearchMode {
	case "opensearch", "hybrid":
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		return "kiberag-documents"
	default:
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		if cfg.AWSS3VectorIndex != "" {
			return cfg.AWSS3VectorIndex
		}
		return "kiberag-documents"
	}
}

func getFusionMethod(opts QueryOptions) opensearch.FusionMethod {
	switch opts.FusionMethod {
	case "weighted_sum":
		return opensearch.FusionMethodWeightedSum
	case "max_score":
		return opensearch.FusionMethodMaxScore
	default:
		return opensearch.FusionMethodRRF
	}
}

// ParseFilters parses a JSON filter string into a string map.
func ParseFilters(filterJSON string) (map[string]string, error) {
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

// OutputHybridResults outputs a single search result set (no Slack).
func OutputHybridResults(result *opensearch.HybridSearchResult, searchType string, opts QueryOptions) error {
	return outputCombinedResultsWithURLContext(result, nil, nil, searchType, opts)
}

// OutputCombinedResults outputs combined document + Slack results.
// Exported for use in tests.
func OutputCombinedResults(result *opensearch.HybridSearchResult, slackResult *slacksearch.SlackSearchResult, searchType string, queryText string, outputJSON bool, urlMessages []slacksearch.EnrichedMessage) error {
	opts := QueryOptions{QueryText: queryText, OutputJSON: outputJSON}
	return outputCombinedResultsWithURLContext(result, slackResult, urlMessages, searchType, opts)
}

func outputCombinedResultsWithURLContext(result *opensearch.HybridSearchResult, slackResult *slacksearch.SlackSearchResult, urlMessages []slacksearch.EnrichedMessage, searchType string, opts QueryOptions) error {
	if opts.OutputJSON {
		payload := struct {
			*opensearch.HybridSearchResult
			Query               string                         `json:"query"`
			Mode                string                         `json:"mode"`
			SlackResults        *slacksearch.SlackSearchResult `json:"slack_results,omitempty"`
			ReferencedSlackURLs []slacksearch.EnrichedMessage  `json:"referenced_slack_urls,omitempty"`
		}{
			HybridSearchResult:  result,
			Query:               opts.QueryText,
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

	if len(urlMessages) > 0 {
		printSlackURLContext(urlMessages)
	}

	PrintHybridResults(result, searchType, opts.QueryText)
	if slackResult != nil {
		printSlackResults(slackResult)
	}

	return nil
}

// PrintHybridResults prints hybrid search results to stdout.
// Exported for tests.
func PrintHybridResults(result *opensearch.HybridSearchResult, searchType string, queryText string) {
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

func outputSlackOnlyResults(result *slacksearch.SlackSearchResult, query string, outputJSON bool) error {
	if outputJSON {
		payload := struct {
			Query  string                         `json:"query"`
			Mode   string                         `json:"mode"`
			Result *slacksearch.SlackSearchResult `json:"result"`
		}{
			Query:  query,
			Mode:   "slack_only",
			Result: result,
		}
		jsonOutput, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("\nQuery: %s\n", query)
	fmt.Println("Search Type: slack_only")

	if result == nil {
		fmt.Println("  (no results)")
		return nil
	}

	fmt.Printf("Execution Time: %v\n", result.ExecutionTime)
	fmt.Printf("Iterations: %d | Total Matches: %d\n", result.IterationCount, result.TotalMatches)

	if len(result.Queries) > 0 {
		fmt.Printf("Queries tried: %s\n", strings.Join(result.Queries, ", "))
	}
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		fmt.Printf("Missing info: %s\n", strings.Join(result.MissingInfo, "; "))
	}

	if len(result.EnrichedMessages) == 0 {
		fmt.Println("\n  (no Slack messages found)")
		return nil
	}

	fmt.Println("\nResults:")
	for i, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n",
			i+1,
			channelName(orig.Channel),
			humanTimestamp(orig.Timestamp),
			displayUser(orig.User, orig.Username),
		)
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     Permalink: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n",
					humanTimestamp(reply.Timestamp),
					strings.TrimSpace(reply.Text),
				)
			}
		}
	}

	return nil
}

// printSlackResults prints Slack search results to stdout.
func printSlackResults(result *slacksearch.SlackSearchResult) {
	fmt.Println("\n=== Slack Conversations ===")
	if result == nil || len(result.EnrichedMessages) == 0 {
		fmt.Println("  (no Slack messages found)")
		return
	}

	fmt.Printf("Iterations: %d | Total Matches: %d\n", result.IterationCount, result.TotalMatches)
	if len(result.Queries) > 0 {
		fmt.Printf("Queries tried: %s\n", strings.Join(result.Queries, ", "))
	}
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		fmt.Printf("Missing info: %s\n", strings.Join(result.MissingInfo, "; "))
	}

	for i, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n", i+1, channelName(orig.Channel), humanTimestamp(orig.Timestamp), displayUser(orig.User, orig.Username))
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     Permalink: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n", humanTimestamp(reply.Timestamp), strings.TrimSpace(reply.Text))
			}
		}
	}
}

// printSlackURLContext prints fetched Slack URL context for CLI output.
func printSlackURLContext(messages []slacksearch.EnrichedMessage) {
	if len(messages) == 0 {
		return
	}

	fmt.Println("\n=== Referenced Slack Messages ===")
	for i, msg := range messages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n",
			i+1,
			channelName(orig.Channel),
			humanTimestamp(orig.Timestamp),
			displayUser(orig.User, orig.Username),
		)
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     URL: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n",
					humanTimestamp(reply.Timestamp),
					strings.TrimSpace(reply.Text),
				)
			}
		}
	}
}

// slackURLContextForPrompt formats fetched Slack URL messages for LLM prompt.
func slackURLContextForPrompt(messages []slacksearch.EnrichedMessage) string {
	return slacksearch.FormatFetchedContext(messages)
}

// slackContextForPrompt formats Slack search results for LLM prompt.
func slackContextForPrompt(result *slacksearch.SlackSearchResult) string {
	return result.ForPrompt()
}

// channelName returns the channel display name.
func channelName(id string) string {
	if id == "" {
		return "-"
	}
	return id
}

// displayUser returns a displayable user string.
func displayUser(userID, username string) string {
	if username != "" {
		return username
	}
	if userID != "" {
		return userID
	}
	return "unknown"
}

// humanTimestamp converts a Slack timestamp to RFC3339 format.
func humanTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	seconds, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return ts
	}
	secs := int64(seconds)
	nsecs := int64((seconds - math.Floor(seconds)) * 1e9)
	return time.Unix(secs, nsecs).Format(time.RFC3339)
}

// Default Slack operation implementations.

func defaultSlackSearch(
	ctx context.Context,
	cfg *appconfig.Config,
	awsCfg aws.Config,
	embeddingClient opensearch.EmbeddingClient,
	userQuery string,
	channels []string,
	progressHandler func(int, int),
) (*slacksearch.SlackSearchResult, error) {
	if !cfg.SlackSearchEnabled {
		return nil, fmt.Errorf("slack search disabled in configuration")
	}

	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot token (SLACK_BOT_TOKEN) not configured")
	}
	if strings.TrimSpace(cfg.SlackUserToken) == "" {
		return nil, fmt.Errorf("slack user token (SLACK_USER_TOKEN) not configured")
	}

	slackClient := slack.New(slackCfg.BotToken)
	chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)

	slackService, err := slacksearch.NewSlackSearchService(cfg, slackClient, chatClient, log.Default())
	if err != nil {
		return nil, err
	}
	if progressHandler != nil {
		slackService.SetProgressHandler(progressHandler)
	}
	if err := slackService.Initialize(ctx); err != nil {
		return nil, err
	}

	sanitized := sanitizeChannels(channels)
	return slackService.Search(ctx, userQuery, sanitized)
}

func defaultSlackOnlySearch(
	ctx context.Context,
	cfg *appconfig.Config,
	awsCfg aws.Config,
	userQuery string,
	channels []string,
	progressHandler func(int, int),
) (*slacksearch.SlackSearchResult, error) {
	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot token (SLACK_BOT_TOKEN) not configured")
	}
	if strings.TrimSpace(cfg.SlackUserToken) == "" {
		return nil, fmt.Errorf("slack user token (SLACK_USER_TOKEN) not configured")
	}

	slackClient := slack.New(slackCfg.BotToken)
	chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)

	originalEnabled := cfg.SlackSearchEnabled
	cfg.SlackSearchEnabled = true
	defer func() { cfg.SlackSearchEnabled = originalEnabled }()

	slackService, err := slacksearch.NewSlackSearchService(cfg, slackClient, chatClient, log.Default())
	if err != nil {
		return nil, fmt.Errorf("failed to create Slack search service: %w", err)
	}
	if progressHandler != nil {
		slackService.SetProgressHandler(progressHandler)
	}
	if err := slackService.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize Slack search service: %w", err)
	}

	sanitized := sanitizeChannels(channels)
	return slackService.Search(ctx, userQuery, sanitized)
}

func defaultFetchSlackURLContext(
	ctx context.Context,
	cfg *appconfig.Config,
	userQuery string,
) ([]slacksearch.EnrichedMessage, error) {
	if !slacksearch.HasSlackURL(userQuery) {
		return nil, nil
	}

	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN not configured for URL fetching")
	}

	slackClient := slack.New(slackCfg.BotToken)
	fetcherConfig := &slacksearch.SlackSearchConfig{
		TimeoutSeconds: cfg.SlackSearchTimeoutSeconds,
	}
	if fetcherConfig.TimeoutSeconds <= 0 {
		fetcherConfig.TimeoutSeconds = 10
	}

	fetcher := slacksearch.NewMessageFetcher(slackClient, nil, fetcherConfig, log.Default())

	urls := slacksearch.DetectSlackURLs(userQuery)
	if len(urls) == 0 {
		return nil, nil
	}

	log.Printf("Detected %d Slack URL(s) in query, fetching content...", len(urls))

	response, err := fetcher.FetchByURLs(ctx, &slacksearch.FetchRequest{
		URLs:      urls,
		UserQuery: userQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Slack messages: %w", err)
	}

	if len(response.Errors) > 0 {
		log.Printf("Slack URL fetch warnings: %v", response.Errors)
	}

	return response.EnrichedMessages, nil
}

// buildEvalRecordFromHybridResult constructs an EvalRecord from a hybrid search result.
func buildEvalRecordFromHybridResult(opts QueryOptions, cfg *appconfig.Config, result *opensearch.HybridSearchResult) *evalexport.EvalRecord {
	record := evalexport.NewEvalRecord("query", opts.QueryText)

	record.RunConfig = evalexport.RunConfig{
		SearchMode:         opts.SearchMode,
		BM25Weight:         opts.BM25Weight,
		VectorWeight:       opts.VectorWeight,
		FusionMethod:       opts.FusionMethod,
		TopK:               opts.TopK,
		IndexName:          getIndexName(cfg, opts),
		UseJapaneseNLP:     opts.UseJapaneseNLP,
		ChatModel:          cfg.ChatModel,
		EmbeddingModel:     "amazon.titan-embed-text-v2:0",
		SlackSearchEnabled: cfg.SlackSearchEnabled,
	}

	record.Timing = evalexport.Timing{
		TotalMs:     result.ExecutionTime.Milliseconds(),
		EmbeddingMs: result.EmbeddingTime.Milliseconds(),
		BM25Ms:      result.BM25Time.Milliseconds(),
		VectorMs:    result.VectorTime.Milliseconds(),
		FusionMs:    result.FusionTime.Milliseconds(),
	}

	if result.FusionResult == nil {
		return record
	}

	docs := make([]evalexport.RetrievedDoc, 0, len(result.FusionResult.Documents))
	for _, doc := range result.FusionResult.Documents {
		rdoc := evalexport.RetrievedDoc{
			DocID:       doc.ID,
			Rank:        doc.Rank,
			FusedScore:  doc.FusedScore,
			BM25Score:   doc.BM25Score,
			VectorScore: doc.VectorScore,
			SearchType:  doc.SearchType,
		}

		if doc.Source != nil {
			var src map[string]interface{}
			if err := json.Unmarshal(doc.Source, &src); err == nil {
				if v, ok := src["title"].(string); ok {
					rdoc.Title = v
				}
				if v, ok := src["file_path"].(string); ok {
					rdoc.SourceFile = v
				}
				if v, ok := src["content"].(string); ok {
					rdoc.Text = v
				}
			}
		}

		docs = append(docs, rdoc)
	}
	record.RetrievedDocs = docs

	contexts := make([]string, 0, len(docs))
	for _, d := range docs {
		if d.Text != "" {
			contexts = append(contexts, d.Text)
		}
	}
	record.RetrievedContexts = contexts

	// References は query では空 map (SearchResponse から取得できないため)
	record.References = map[string]string{}

	return record
}

// sanitizeChannels strips leading '#' from channel names.
func sanitizeChannels(channels []string) []string {
	if len(channels) == 0 {
		return nil
	}
	clean := make([]string, 0, len(channels))
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		ch = strings.TrimPrefix(ch, "#")
		if ch != "" {
			clean = append(clean, ch)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}
