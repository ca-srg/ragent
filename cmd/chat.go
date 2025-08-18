package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/kiberag/internal/config"
	"github.com/ca-srg/kiberag/internal/embedding/bedrock"
	"github.com/ca-srg/kiberag/internal/filter"
	"github.com/ca-srg/kiberag/internal/s3vector"
	commontypes "github.com/ca-srg/kiberag/internal/types"
)

var (
	contextSize  int
	interactive  bool
	systemPrompt string
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive chat using S3 Vector for context and Bedrock for responses",
	Long: `
Start an interactive chat session that uses your S3 Vector Index for context retrieval
and Amazon Bedrock (Claude Sonnet 4) for generating responses.

The chat will search your indexed documents for relevant context based on your questions
and provide informed responses using the retrieved information.

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
}

func runChat(cmd *cobra.Command, args []string) error {
	log.Println("Starting chat session...")

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
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

	// Create S3 Vector service
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           cfg.AWSS3Region,
	}

	s3Service, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return fmt.Errorf("failed to create S3 Vector service: %w", err)
	}

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

	if err := s3Service.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("S3 Vector access validation failed: %w", err)
	}

	log.Printf("Chat ready! Using model: %s", cfg.ChatModel)
	fmt.Println("=== Kiberag Chat Session ===")
	fmt.Println("Type 'exit' or 'quit' to end the session")
	fmt.Println("Type 'help' for available commands")
	fmt.Println("=============================")
	fmt.Println()

	return startChatLoop(chatClient, embeddingClient, s3Service, cfg)
}

func startChatLoop(chatClient, embeddingClient *bedrock.BedrockClient, s3Service *s3vector.S3VectorService, cfg *commontypes.Config) error {
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
		response, err := generateChatResponse(userInput, conversationHistory, chatClient, embeddingClient, s3Service, cfg)
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

func generateChatResponse(userInput string, history []bedrock.ChatMessage, chatClient, embeddingClient *bedrock.BedrockClient, s3Service *s3vector.S3VectorService, cfg *commontypes.Config) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Generate embedding for user input to find relevant context
	log.Printf("Searching for relevant context...")
	queryEmbedding, err := embeddingClient.GenerateEmbedding(ctx, userInput)
	if err != nil {
		return "", fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Build filter with exclusions
	excludeFilter, err := filter.BuildExclusionFilter(cfg, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build exclusion filter: %w", err)
	}

	// Log filter for debugging
	if len(cfg.ExcludeCategories) > 0 {
		log.Printf("Excluding categories from chat context: %v", cfg.ExcludeCategories)
	}

	// Search for relevant context
	searchResult, err := s3Service.QueryVectors(ctx, queryEmbedding, contextSize, excludeFilter)
	if err != nil {
		return "", fmt.Errorf("failed to search for context: %w", err)
	}

	// Build context from search results and collect references
	var contextParts []string
	references := make(map[string]string) // title -> reference URL
	for _, result := range searchResult.Results {
		if result.Metadata != nil {
			if content, ok := result.Metadata["content"].(string); ok {
				contextParts = append(contextParts, content)
			}
			// Collect title and reference URLs
			var title, reference string
			if t, ok := result.Metadata["title"].(string); ok {
				title = t
			}
			if ref, ok := result.Metadata["reference"].(string); ok && ref != "" {
				reference = ref
			}
			// Only add if both title and reference exist
			if title != "" && reference != "" {
				references[title] = reference
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

	// Add retrieved context
	if len(contextParts) > 0 {
		if len(history) == 0 && systemPrompt != "" {
			contextualPrompt = fmt.Sprintf("System: %s\n\nContext information:\n%s\n\nUser question: %s",
				systemPrompt, strings.Join(contextParts, "\n\n---\n\n"), userInput)
		} else {
			contextualPrompt = fmt.Sprintf("Context information:\n%s\n\nUser question: %s",
				strings.Join(contextParts, "\n\n---\n\n"), userInput)
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
