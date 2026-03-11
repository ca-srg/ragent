package cmd

import (
	"log"

	"github.com/spf13/cobra"

	queryimpl "github.com/ca-srg/ragent/internal/query"
)

// QuerySearchClient is a type alias for backward compatibility with existing tests.
// The canonical definition lives in internal/query.
type QuerySearchClient = queryimpl.QuerySearchClient

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
	exportEval     bool
	exportEvalPath string
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
	RunE: func(cmd *cobra.Command, args []string) error {
		return queryimpl.RunQuery(cmd, queryimpl.QueryOptions{
			QueryText:      queryText,
			TopK:           topK,
			OutputJSON:     outputJSON,
			FilterQuery:    filterQuery,
			SearchMode:     searchMode,
			IndexName:      indexName,
			BM25Weight:     bm25Weight,
			VectorWeight:   vectorWeight,
			FusionMethod:   fusionMethod,
			UseJapaneseNLP: useJapaneseNLP,
			Timeout:        timeout,
			OnlySlack:      queryOnlySlack,
			SlackChannels:  slackChannels,
			ExportEval:     exportEval,
			ExportEvalPath: exportEvalPath,
		})
	},
}

func init() {
	queryCmd.Flags().StringVarP(&queryText, "query", "q", "", "Text query to search for (required)")
	queryCmd.Flags().IntVarP(&topK, "top-k", "k", 10, "Number of similar results to return")
	queryCmd.Flags().BoolVarP(&outputJSON, "json", "j", false, "Output results in JSON format")
	queryCmd.Flags().StringVarP(&filterQuery, "filter", "f", "", "JSON metadata filter (e.g., '{\"category\":\"docs\"}')")

	// Hybrid search flags
	queryCmd.Flags().StringVar(&searchMode, "search-mode", "hybrid", "Search mode: hybrid|opensearch")
	queryCmd.Flags().StringVar(&indexName, "index-name", "", "OpenSearch index name (optional, defaults to config)")
	queryCmd.Flags().Float64Var(&bm25Weight, "bm25-weight", 0.5, "BM25 search weight in hybrid mode (0.0-1.0)")
	queryCmd.Flags().Float64Var(&vectorWeight, "vector-weight", 0.5, "Vector search weight in hybrid mode (0.0-1.0)")
	queryCmd.Flags().StringVar(&fusionMethod, "fusion-method", "rrf", "Result fusion method: rrf|weighted_sum|max_score")
	queryCmd.Flags().BoolVar(&useJapaneseNLP, "japanese-nlp", false, "Enable Japanese text processing and analysis")
	queryCmd.Flags().IntVar(&timeout, "timeout", 30, "Request timeout in seconds")
	queryCmd.Flags().BoolVar(&queryOnlySlack, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")
	queryCmd.Flags().StringSliceVar(&slackChannels, "slack-channels", nil, "Limit Slack search to specific channel names (omit leading #)")
	queryCmd.Flags().BoolVar(&exportEval, "export-eval", false, "Enable evaluation data export")
	queryCmd.Flags().StringVar(&exportEvalPath, "export-eval-path", "./evaluation/exports/", "Output directory for JSONL evaluation data")

	if err := queryCmd.MarkFlagRequired("query"); err != nil {
		log.Fatalf("Failed to mark query flag as required: %v", err)
	}
}
