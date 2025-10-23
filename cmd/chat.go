package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/search"
	"github.com/ca-srg/ragent/internal/slacksearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

var (
	contextSize        int
	interactive        bool
	systemPrompt       string
	chatBM25Weight     float64
	chatVectorWeight   float64
	chatUseJapaneseNLP bool
)

type chatResponder interface {
	GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error)
}

type hybridSearchInitializer interface {
	Initialize(ctx context.Context) error
	Search(ctx context.Context, request *search.SearchRequest) (*search.SearchResponse, error)
}

var newHybridSearchServiceFunc = func(cfg *commontypes.Config, embeddingClient *bedrock.BedrockClient) (hybridSearchInitializer, error) {
	return search.NewHybridSearchService(cfg, embeddingClient, nil, nil)
}

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
	RunE: runChat,
}

func init() {
	chatCmd.Flags().IntVarP(&contextSize, "context-size", "c", 5, "Number of context documents to retrieve")
	chatCmd.Flags().BoolVarP(&interactive, "interactive", "i", true, "Run in interactive mode")
	chatCmd.Flags().StringVarP(&systemPrompt, "system", "s", "", "System prompt for the chat")
	chatCmd.Flags().Float64VarP(&chatBM25Weight, "bm25-weight", "b", 0.5, "Weight for BM25 scoring in hybrid search (0-1)")
	chatCmd.Flags().Float64VarP(&chatVectorWeight, "vector-weight", "v", 0.5, "Weight for vector scoring in hybrid search (0-1)")
	chatCmd.Flags().BoolVar(&chatUseJapaneseNLP, "use-japanese-nlp", true, "Use Japanese NLP optimization for OpenSearch")
}

func runChat(cmd *cobra.Command, args []string) error {
	log.Println("Starting chat session...")

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Require OpenSearch configuration for chat (fallback removed)
	if cfg.OpenSearchEndpoint == "" {
		return fmt.Errorf("OpenSearch is required for chat: set OPENSEARCH_ENDPOINT and related settings")
	}

	// Load AWS configuration - FIXED to us-east-1 for Bedrock chat functionality
	// Note: This is intentionally hardcoded and does not use cfg.AWSS3Region
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create Bedrock client for chat
	chatClient := bedrock.NewBedrockClient(awsConfig, cfg.ChatModel)

	// Create embedding client for context retrieval
	embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Validate connections
	log.Println("Validating service connections...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := chatClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("chat service validation failed: %w", err)
	}

	if err := embeddingClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("embedding service validation failed: %w", err)
	}

	// Optionally validate OpenSearch is reachable early
	if err := validateOpenSearch(ctx, cfg, embeddingClient); err != nil {
		return err
	}

	log.Printf("Chat ready! Using model: %s", cfg.ChatModel)
	fmt.Println("=== Kiberag Chat Session ===")
	fmt.Println("Type 'exit' or 'quit' to end the session")
	fmt.Println("Type 'help' for available commands")
	fmt.Println("=============================")
	fmt.Println()

	return startChatLoop(chatClient, embeddingClient, cfg, awsConfig)
}

func startChatLoop(chatClient chatResponder, embeddingClient *bedrock.BedrockClient, cfg *commontypes.Config, awsCfg aws.Config) error {
	scanner := bufio.NewScanner(os.Stdin)
	var conversationHistory []bedrock.ChatMessage

	// Note: System prompt will be added to the first user message context instead of using "system" role
	// because Bedrock Claude API only supports "user" and "assistant" roles

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())

		// Handle special commands
		switch strings.ToLower(userInput) {
		case "exit", "quit":
			fmt.Println("Goodbye!")
			return nil
		case "help":
			printChatHelp()
			continue
		case "clear":
			conversationHistory = nil
			fmt.Println("Conversation history cleared.")
			continue
		case "":
			continue
		}

		// Generate response
		response, err := generateChatResponse(userInput, conversationHistory, chatClient, embeddingClient, cfg, awsCfg, cfg.SlackSearchEnabled)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		// Add to conversation history
		conversationHistory = append(conversationHistory,
			bedrock.ChatMessage{Role: "user", Content: userInput},
			bedrock.ChatMessage{Role: "assistant", Content: response},
		)

		fmt.Printf("Assistant: %s\n\n", response)
	}

	return scanner.Err()
}

func generateChatResponse(userInput string, history []bedrock.ChatMessage, chatClient chatResponder, embeddingClient *bedrock.BedrockClient, cfg *commontypes.Config, awsCfg aws.Config, slackEnabled bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if slackEnabled {
		fmt.Println("Searching documents and Slack conversations...")
	} else {
		fmt.Println("Searching documents...")
	}

	// Create and initialize hybrid search service
	log.Printf("Searching for relevant context using hybrid mode...")
	searchService, err := newHybridSearchServiceFunc(cfg, embeddingClient)
	if err != nil {
		return "", fmt.Errorf("failed to create search service: %w", err)
	}

	if err := searchService.Initialize(ctx); err != nil {
		return "", fmt.Errorf("failed to initialize search service: %w", err)
	}

	// Prepare search request with same parameters as original searchWithHybrid
	searchRequest := &search.SearchRequest{
		Query:          userInput,
		IndexName:      getIndexNameForChat(cfg, "hybrid"),
		ContextSize:    contextSize,
		BM25Weight:     chatBM25Weight,
		VectorWeight:   chatVectorWeight,
		UseJapaneseNLP: chatUseJapaneseNLP,
		TimeoutSeconds: 30,
	}

	// Execute search using service
	searchResponse, err := searchService.Search(ctx, searchRequest)
	if err != nil {
		return "", fmt.Errorf("failed to search for context: %w", err)
	}

	contextParts := searchResponse.ContextParts
	references := searchResponse.References

	var slackResult *slacksearch.SlackSearchResult
	if slackEnabled {
		var slackErr error
		slackResult, slackErr = slackSearchRunner(ctx, cfg, awsCfg, embeddingClient, userInput, nil, func(iteration, max int) {
			fmt.Printf("Refining Slack search (iteration %d/%d)...\n", iteration, max)
		})
		if slackErr != nil {
			fmt.Printf("Slack search unavailable: %v\n", slackErr)
		} else if slackResult != nil {
			fmt.Printf("Slack search completed in %d iteration(s).\n", slackResult.IterationCount)
			printSlackResults(slackResult)
			if slackPrompt := slackContextForPrompt(slackResult); slackPrompt != "" {
				contextParts = append(contextParts, slackPrompt)
			}
		}
	}

	// Prepare messages with context
	messages := make([]bedrock.ChatMessage, len(history))
	copy(messages, history)

	// Build contextual prompt with system prompt if provided
	contextualPrompt := userInput

	// Add system prompt at the beginning if this is the first message and system prompt is set
	if len(history) == 0 && systemPrompt != "" {
		contextualPrompt = fmt.Sprintf("System: %s\n\n%s", systemPrompt, userInput)
	}

	// Add retrieved context with explicit instructions to use it
	if len(contextParts) > 0 {
		// Create a strong instruction to use the retrieved context
		ragInstruction := "以下の参考文献は、あなたの質問に関連する社内ドキュメントから検索されたものです。" +
			"必ずこれらの参考文献の内容に基づいて回答してください。" +
			"一般的な知識ではなく、提供された参考文献の具体的な内容を優先して使用してください。"

		if len(history) == 0 && systemPrompt != "" {
			contextualPrompt = fmt.Sprintf("System: %s\n\n%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
				systemPrompt, ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), userInput)
		} else {
			contextualPrompt = fmt.Sprintf("%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
				ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), userInput)
		}
	}

	messages = append(messages, bedrock.ChatMessage{
		Role:    "user",
		Content: contextualPrompt,
	})

	// Generate response using chat client
	log.Printf("Generating response...")
	response, err := chatClient.GenerateChatResponse(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate chat response: %w", err)
	}

	// Append references and Slack context for the user-facing answer
	if len(references) > 0 || (slackResult != nil && len(slackResult.EnrichedMessages) > 0) || slackEnabled {
		var builder strings.Builder
		builder.WriteString(response)

		if len(references) > 0 {
			builder.WriteString("\n\n## 参考文献\n\n")
			for title, ref := range references {
				builder.WriteString(fmt.Sprintf("- %s: %s\n", title, ref))
			}
		}

		if slackResult != nil && len(slackResult.EnrichedMessages) > 0 {
			builder.WriteString("\n\n## Slack Conversations\n\n")
			for _, msg := range slackResult.EnrichedMessages {
				orig := msg.OriginalMessage
				builder.WriteString(fmt.Sprintf("- #%s (%s): %s", channelName(orig.Channel), humanTimestamp(orig.Timestamp), strings.TrimSpace(orig.Text)))
				if msg.Permalink != "" {
					builder.WriteString(fmt.Sprintf(" (%s)", msg.Permalink))
				}
				builder.WriteString("\n")
			}
		} else if slackEnabled {
			builder.WriteString("\n\n## Slack Conversations\n\n- No Slack conversations found.\n")
		}

		response = builder.String()
	}

	return response, nil
}

func printChatHelp() {
	fmt.Println("\nAvailable commands:")
	fmt.Println("  exit, quit  - End the chat session")
	fmt.Println("  clear       - Clear conversation history")
	fmt.Println("  help        - Show this help message")
	fmt.Println()
}

// validateOpenSearch performs a quick connectivity check
func validateOpenSearch(ctx context.Context, cfg *commontypes.Config, embeddingClient *bedrock.BedrockClient) error {
	osConfig, err := opensearch.NewConfigFromTypes(cfg)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch config: %w", err)
	}
	if err := osConfig.Validate(); err != nil {
		return fmt.Errorf("OpenSearch config validation failed: %w", err)
	}
	osClient, err := opensearch.NewClient(osConfig)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}
	if err := osClient.HealthCheck(ctx); err != nil {
		return fmt.Errorf("OpenSearch health check failed: %w", err)
	}
	// Quick embedding validation already done separately; return success
	return nil
}

// searchWithOpenSearchOnly performs search using OpenSearch BM25 only
// Removed OpenSearch-only path; chat uses hybrid search exclusively

// getIndexNameForChat returns the index name for chat queries based on search mode
func getIndexNameForChat(cfg *commontypes.Config, searchMode string) string {
	switch searchMode {
	case "opensearch", "hybrid":
		// Use OpenSearch index for OpenSearch-based searches
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		return "kiberag-documents"
	default:
		// Default to OpenSearch index
		if cfg.OpenSearchIndex != "" {
			return cfg.OpenSearchIndex
		}
		return "kiberag-documents"
	}
}
