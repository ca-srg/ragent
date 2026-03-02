package cmd

import (
	"github.com/spf13/cobra"

	queryimpl "github.com/ca-srg/ragent/internal/query"
)

var (
	contextSize        int
	interactive        bool
	systemPrompt       string
	chatBM25Weight     float64
	chatVectorWeight   float64
	chatUseJapaneseNLP bool
	chatOnlySlack      bool
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive chat using OpenSearch hybrid search for context",
	Long: `
Start an interactive chat session that uses OpenSearch hybrid search (BM25 + vector)
for context retrieval and Amazon Bedrock (Claude Sonnet 4) for generating responses.

The chat searches your OpenSearch index for relevant context and answers using the
retrieved information. OpenSearch configuration is required.

Examples:
  kiberag chat                           # Start interactive chat
  kiberag chat --context-size 10        # Use more context documents
  kiberag chat --system "You are a helpful assistant specialized in documentation."
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return queryimpl.RunChat(cmd, queryimpl.ChatOptions{
			ContextSize:    contextSize,
			Interactive:    interactive,
			SystemPrompt:   systemPrompt,
			BM25Weight:     chatBM25Weight,
			VectorWeight:   chatVectorWeight,
			UseJapaneseNLP: chatUseJapaneseNLP,
			OnlySlack:      chatOnlySlack,
		})
	},
}

func init() {
	chatCmd.Flags().IntVarP(&contextSize, "context-size", "c", 5, "Number of context documents to retrieve")
	chatCmd.Flags().BoolVarP(&interactive, "interactive", "i", true, "Run in interactive mode")
	chatCmd.Flags().StringVarP(&systemPrompt, "system", "s", "", "System prompt for the chat")
	chatCmd.Flags().Float64VarP(&chatBM25Weight, "bm25-weight", "b", 0.5, "Weight for BM25 scoring in hybrid search (0-1)")
	chatCmd.Flags().Float64VarP(&chatVectorWeight, "vector-weight", "v", 0.5, "Weight for vector scoring in hybrid search (0-1)")
	chatCmd.Flags().BoolVar(&chatUseJapaneseNLP, "use-japanese-nlp", true, "Use Japanese NLP optimization for OpenSearch")
	chatCmd.Flags().BoolVar(&chatOnlySlack, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")
}
