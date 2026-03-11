package query

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/evalexport"
	"github.com/ca-srg/ragent/internal/pkg/metrics"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/search"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

// ChatResponder defines the interface for generating chat responses.
type ChatResponder interface {
	GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error)
}

// HybridSearchInitializer defines the interface for initializing and using hybrid search.
type HybridSearchInitializer interface {
	Initialize(ctx context.Context) error
	Search(ctx context.Context, request *search.SearchRequest) (*search.SearchResponse, error)
}

// NewHybridSearchServiceFunc is the factory for creating hybrid search services.
// Injectable for tests.
var NewHybridSearchServiceFunc = func(cfg *appconfig.Config, embeddingClient embedding.EmbeddingClient) (HybridSearchInitializer, error) {
	return search.NewHybridSearchService(cfg, embeddingClient, nil, nil)
}

// ChatOptions holds all flag values from cmd/chat.go.
type ChatOptions struct {
	ContextSize    int
	Interactive    bool
	SystemPrompt   string
	BM25Weight     float64
	VectorWeight   float64
	UseJapaneseNLP bool
	OnlySlack      bool
	ExportEval     bool
	ExportEvalPath string
}

// ChatResult holds the result of a single GenerateChatResponse call.
type ChatResult struct {
	Response     string
	ContextParts []string
	References   map[string]string
	LLMMs        int64
}

// RunChat is the exported entry point called from cmd/chat.go.
func RunChat(cmd *cobra.Command, opts ChatOptions) error {
	metrics.RecordInvocation(metrics.ModeChat)
	log.Println("Starting chat session...")

	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if !opts.OnlySlack {
		if cfg.OpenSearchEndpoint == "" {
			return fmt.Errorf("OpenSearch is required for chat: set OPENSEARCH_ENDPOINT and related settings (use --only-slack to skip)")
		}
	} else {
		cfg.SlackSearchEnabled = true
		log.Println("Running in Slack-only mode (OpenSearch disabled)")
	}

	bedrockConfig, err := bedrock.BuildBedrockAWSConfig(context.TODO(), cfg.BedrockRegion, cfg.BedrockBearerToken)
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	chatClient := bedrock.NewBedrockClient(bedrockConfig, cfg.ChatModel)
	embeddingClient, err := embedding.NewEmbeddingClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create embedding client: %w", err)
	}

	log.Println("Validating service connections...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := chatClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("chat service validation failed: %w", err)
	}

	if err := embeddingClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("embedding service validation failed: %w", err)
	}

	if !opts.OnlySlack {
		if err := validateOpenSearch(ctx, cfg, embeddingClient); err != nil {
			return err
		}
	}

	log.Printf("Chat ready! Using model: %s", cfg.ChatModel)
	if opts.OnlySlack {
		fmt.Println("=== Kiberag Chat Session (Slack Only) ===")
	} else {
		fmt.Println("=== Kiberag Chat Session ===")
	}
	fmt.Println("Type 'exit' or 'quit' to end the session")
	fmt.Println("Type 'help' for available commands")
	fmt.Println("=============================")
	fmt.Println()

	return startChatLoop(chatClient, embeddingClient, cfg, bedrockConfig, opts)
}

func startChatLoop(chatClient ChatResponder, embeddingClient embedding.EmbeddingClient, cfg *appconfig.Config, awsCfg aws.Config, opts ChatOptions) error {
	scanner := bufio.NewScanner(os.Stdin)
	var conversationHistory []bedrock.ChatMessage

	// Note: System prompt will be added to the first user message context instead of using "system" role
	// because Bedrock Claude API only supports "user" and "assistant" roles

	var evalWriter *evalexport.Writer
	if opts.ExportEval {
		var werr error
		evalWriter, werr = evalexport.NewWriter(opts.ExportEvalPath)
		if werr != nil {
			log.Printf("Warning: failed to create eval export writer: %v", werr)
		}
	}

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())

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

		turnStart := time.Now()
		result, err := GenerateChatResponse(userInput, conversationHistory, chatClient, embeddingClient, cfg, awsCfg, cfg.SlackSearchEnabled, opts)
		turnMs := time.Since(turnStart).Milliseconds()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		conversationHistory = append(conversationHistory,
			bedrock.ChatMessage{Role: "user", Content: userInput},
			bedrock.ChatMessage{Role: "assistant", Content: result.Response},
		)

		fmt.Printf("Assistant: %s\n\n", result.Response)

		if evalWriter != nil {
			record := evalexport.NewEvalRecord("chat", userInput)
			record.Response = result.Response
			record.RetrievedContexts = result.ContextParts
			record.References = result.References
			record.Timing = evalexport.Timing{
				TotalMs: turnMs,
				LLMMs:   result.LLMMs,
			}
			record.RunConfig = evalexport.RunConfig{
				SearchMode:         "hybrid",
				BM25Weight:         opts.BM25Weight,
				VectorWeight:       opts.VectorWeight,
				FusionMethod:       "weighted_sum",
				TopK:               opts.ContextSize,
				IndexName:          getIndexNameForChat(cfg, "hybrid"),
				UseJapaneseNLP:     opts.UseJapaneseNLP,
				ChatModel:          cfg.ChatModel,
				EmbeddingModel:     "amazon.titan-embed-text-v2:0",
				SlackSearchEnabled: cfg.SlackSearchEnabled,
			}
			if werr := evalWriter.WriteRecord(record); werr != nil {
				log.Printf("Warning: failed to export eval record: %v", werr)
			}
		}
	}

	return scanner.Err()
}

// GenerateChatResponse generates a chat response using hybrid search for context.
// Exported for tests.
func GenerateChatResponse(userInput string, history []bedrock.ChatMessage, chatClient ChatResponder, embeddingClient embedding.EmbeddingClient, cfg *appconfig.Config, awsCfg aws.Config, slackEnabled bool, opts ChatOptions) (*ChatResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var slackURLMessages []slacksearch.EnrichedMessage
	urlMessages, err := FetchSlackURLContext(ctx, cfg, userInput)
	if err != nil {
		log.Printf("Slack URL fetch warning: %v", err)
	} else if len(urlMessages) > 0 {
		slackURLMessages = urlMessages
		fmt.Printf("Fetched %d message(s) from Slack URL(s)\n", len(urlMessages))
		printSlackURLContext(slackURLMessages)
	}

	var contextParts []string
	var references map[string]string
	var slackResult *slacksearch.SlackSearchResult

	if opts.OnlySlack {
		fmt.Println("Searching Slack conversations...")

		if len(slackURLMessages) > 0 {
			urlContext := slackURLContextForPrompt(slackURLMessages)
			if urlContext != "" {
				contextParts = append(contextParts, urlContext)
			}
		}

		var slackErr error
		slackResult, slackErr = SlackSearchRunner(ctx, cfg, awsCfg, embeddingClient, userInput, nil, func(iteration, max int) {
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

		references = make(map[string]string)
	} else {
		if slackEnabled {
			fmt.Println("Searching documents and Slack conversations...")
		} else {
			fmt.Println("Searching documents...")
		}

		log.Printf("Searching for relevant context using hybrid mode...")
		searchService, err := NewHybridSearchServiceFunc(cfg, embeddingClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create search service: %w", err)
		}

		if err := searchService.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize search service: %w", err)
		}

		searchRequest := &search.SearchRequest{
			Query:          userInput,
			IndexName:      getIndexNameForChat(cfg, "hybrid"),
			ContextSize:    opts.ContextSize,
			BM25Weight:     opts.BM25Weight,
			VectorWeight:   opts.VectorWeight,
			UseJapaneseNLP: opts.UseJapaneseNLP,
			TimeoutSeconds: 30,
		}

		searchResponse, err := searchService.Search(ctx, searchRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to search for context: %w", err)
		}

		contextParts = searchResponse.ContextParts
		references = searchResponse.References

		if len(slackURLMessages) > 0 {
			urlContext := slackURLContextForPrompt(slackURLMessages)
			if urlContext != "" {
				// Prepend URL context as it's explicitly referenced by user
				contextParts = append([]string{urlContext}, contextParts...)
			}
		}

		if slackEnabled {
			var slackErr error
			slackResult, slackErr = SlackSearchRunner(ctx, cfg, awsCfg, embeddingClient, userInput, nil, func(iteration, max int) {
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
	}

	messages := make([]bedrock.ChatMessage, len(history))
	copy(messages, history)

	// Build contextual prompt with system prompt if provided
	contextualPrompt := userInput

	// Add system prompt at the beginning if this is the first message and system prompt is set
	if len(history) == 0 && opts.SystemPrompt != "" {
		contextualPrompt = fmt.Sprintf("System: %s\n\n%s", opts.SystemPrompt, userInput)
	}

	// Add retrieved context with explicit instructions to use it
	if len(contextParts) > 0 {
		ragInstruction := "以下の参考文献は、あなたの質問に関連する社内ドキュメントから検索されたものです。" +
			"必ずこれらの参考文献の内容に基づいて回答してください。" +
			"一般的な知識ではなく、提供された参考文献の具体的な内容を優先して使用してください。"

		if len(history) == 0 && opts.SystemPrompt != "" {
			contextualPrompt = fmt.Sprintf("System: %s\n\n%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
				opts.SystemPrompt, ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), userInput)
		} else {
			contextualPrompt = fmt.Sprintf("%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
				ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), userInput)
		}
	}

	messages = append(messages, bedrock.ChatMessage{
		Role:    "user",
		Content: contextualPrompt,
	})

	log.Printf("Generating response...")
	llmStart := time.Now()
	response, err := chatClient.GenerateChatResponse(ctx, messages)
	llmMs := time.Since(llmStart).Milliseconds()
	if err != nil {
		return nil, fmt.Errorf("failed to generate chat response: %w", err)
	}

	// Append references and Slack context for the user-facing answer
	if len(references) > 0 || (slackResult != nil && len(slackResult.EnrichedMessages) > 0) || slackEnabled {
		var builder strings.Builder
		builder.WriteString(response)

		if len(references) > 0 {
			builder.WriteString("\n\n## 参考文献\n\n")
			for title, ref := range references {
				fmt.Fprintf(&builder, "- %s: %s\n", title, ref)
			}
		}

		if slackResult != nil && len(slackResult.EnrichedMessages) > 0 {
			builder.WriteString("\n\n## Slack Conversations\n\n")
			for _, msg := range slackResult.EnrichedMessages {
				orig := msg.OriginalMessage
				fmt.Fprintf(&builder, "- #%s (%s): %s", channelName(orig.Channel), humanTimestamp(orig.Timestamp), strings.TrimSpace(orig.Text))
				if msg.Permalink != "" {
					fmt.Fprintf(&builder, " (%s)", msg.Permalink)
				}
				builder.WriteString("\n")
			}
		} else if slackEnabled {
			builder.WriteString("\n\n## Slack Conversations\n\n- No Slack conversations found.\n")
		}

		response = builder.String()
	}

	return &ChatResult{
		Response:     response,
		ContextParts: contextParts,
		References:   references,
		LLMMs:        llmMs,
	}, nil
}

// printChatHelp prints available chat commands.
func printChatHelp() {
	fmt.Println("\nAvailable commands:")
	fmt.Println("  exit, quit  - End the chat session")
	fmt.Println("  clear       - Clear conversation history")
	fmt.Println("  help        - Show this help message")
	fmt.Println()
}

// validateOpenSearch performs a quick connectivity check.
func validateOpenSearch(ctx context.Context, cfg *appconfig.Config, embeddingClient embedding.EmbeddingClient) error {
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

// getIndexNameForChat returns the index name for chat queries based on search mode.
func getIndexNameForChat(cfg *appconfig.Config, searchMode string) string {
	switch searchMode {
	case "opensearch", "hybrid":
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
