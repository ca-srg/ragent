package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/mdrag/internal/config"
	"github.com/ca-srg/mdrag/internal/embedding/bedrock"
	"github.com/ca-srg/mdrag/internal/opensearch"
	commontypes "github.com/ca-srg/mdrag/internal/types"
)

var (
	contextSize        int
	interactive        bool
	systemPrompt       string
	chatBM25Weight     float64
	chatVectorWeight   float64
	chatUseJapaneseNLP bool
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

	return startChatLoop(chatClient, embeddingClient, cfg)
}

func startChatLoop(chatClient, embeddingClient *bedrock.BedrockClient, cfg *commontypes.Config) error {
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
		response, err := generateChatResponse(userInput, conversationHistory, chatClient, embeddingClient, cfg)
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

func generateChatResponse(userInput string, history []bedrock.ChatMessage, chatClient, embeddingClient *bedrock.BedrockClient, cfg *commontypes.Config) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Generate embedding for user input to find relevant context
	log.Printf("Searching for relevant context using hybrid mode...")
	queryEmbedding64, err := embeddingClient.GenerateEmbedding(ctx, userInput)
	if err != nil {
		return "", fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Use OpenSearch hybrid search only (fallback removed)
	contextParts, references, err := searchWithHybrid(ctx, userInput, queryEmbedding64, cfg, embeddingClient)
	if err != nil {
		return "", fmt.Errorf("failed to search for context: %w", err)
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

	// Add references to response if any were found
	if len(references) > 0 {
		var referenceBuilder strings.Builder
		referenceBuilder.WriteString(response)
		referenceBuilder.WriteString("\n\n## 参考文献\n\n")

		// Display title: reference format
		for title, ref := range references {
			referenceBuilder.WriteString(fmt.Sprintf("- %s: %s\n", title, ref))
		}

		response = referenceBuilder.String()
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

// searchWithHybrid performs hybrid search using OpenSearch
func searchWithHybrid(ctx context.Context, query string, queryEmbedding []float64, cfg *commontypes.Config, embeddingClient *bedrock.BedrockClient) ([]string, map[string]string, error) {
	// Validate OpenSearch configuration
	if cfg.OpenSearchEndpoint == "" {
		return nil, nil, fmt.Errorf("OpenSearch endpoint not configured")
	}

	// Create OpenSearch client
	osConfig, err := opensearch.NewConfigFromTypes(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OpenSearch config: %w", err)
	}

	if err := osConfig.Validate(); err != nil {
		return nil, nil, fmt.Errorf("OpenSearch config validation failed: %w", err)
	}

	osClient, err := opensearch.NewClient(osConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Test connection
	if err := osClient.HealthCheck(ctx); err != nil {
		return nil, nil, fmt.Errorf("OpenSearch health check failed: %w", err)
	}

	// Create hybrid search engine
	hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)

	// Build hybrid query
	hybridQuery := &opensearch.HybridQuery{
		Query:          query,
		IndexName:      getIndexNameForChat(cfg, "hybrid"),
		Size:           contextSize,
		BM25Weight:     chatBM25Weight,
		VectorWeight:   chatVectorWeight,
		FusionMethod:   opensearch.FusionMethodWeightedSum,
		UseJapaneseNLP: chatUseJapaneseNLP,
		TimeoutSeconds: 30,
	}

	// Execute search
	log.Println("Executing OpenSearch hybrid search...")
	result, err := hybridEngine.Search(ctx, hybridQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("hybrid search failed: %w", err)
	}

	// Extract context and references from results
	var contextParts []string
	references := make(map[string]string)

	for _, doc := range result.FusionResult.Documents {
		// Unmarshal the source JSON
		var source map[string]interface{}
		if err := json.Unmarshal(doc.Source, &source); err != nil {
			continue // Skip this document if we can't unmarshal
		}

		// Extract content
		if content, ok := source["content"].(string); ok && content != "" {
			contextParts = append(contextParts, content)
		}

		// Extract title and reference
		var title, reference string
		if t, ok := source["title"].(string); ok {
			title = t
		}
		if ref, ok := source["reference"].(string); ok && ref != "" {
			reference = ref
		}
		if title != "" && reference != "" {
			references[title] = reference
		}
	}

	return contextParts, references, nil
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
