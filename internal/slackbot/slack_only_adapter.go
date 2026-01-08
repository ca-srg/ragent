package slackbot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

// SlackOnlySearchAdapter uses only Slack search without OpenSearch
// This adapter is used in --only-slack mode
type SlackOnlySearchAdapter struct {
	cfg         *commontypes.Config
	maxResults  int
	slackSearch SlackConversationSearcher
	chatClient  *bedrock.BedrockClient

	awsCfg *aws.Config
}

// NewSlackOnlySearchAdapter creates a new Slack-only search adapter
func NewSlackOnlySearchAdapter(
	cfg *commontypes.Config,
	maxResults int,
	slackSearch SlackConversationSearcher,
	chatClient *bedrock.BedrockClient,
	awsCfg *aws.Config,
) *SlackOnlySearchAdapter {
	if maxResults <= 0 {
		maxResults = 5
	}

	return &SlackOnlySearchAdapter{
		cfg:         cfg,
		maxResults:  maxResults,
		slackSearch: slackSearch,
		chatClient:  chatClient,
		awsCfg:      awsCfg,
	}
}

// Search implements SearchAdapter interface using only Slack search
func (s *SlackOnlySearchAdapter) Search(ctx context.Context, query string, opts SearchOptions) *SearchResult {
	start := time.Now()

	// Execute Slack search
	var slackResult *SlackConversationResult
	if s.slackSearch != nil {
		var err error
		slackResult, err = s.slackSearch.SearchConversations(ctx, query, opts)
		if err != nil {
			log.Printf("Slack search error: %v", err)
			slackResult = nil
		}
	}

	// Build context from Slack results
	var contextParts []string
	if slackResult != nil {
		if slackContext := slackResult.ForPrompt(); slackContext != "" {
			contextParts = append(contextParts, slackContext)
		}
	}

	// Generate chat response using LLM
	var generatedResponse string
	if len(contextParts) > 0 {
		generatedResponse = s.generateResponse(ctx, query, contextParts)
	} else {
		generatedResponse = "Slackから関連する情報が見つかりませんでした。"
	}

	total := 0
	if slackResult != nil {
		total = slackResult.TotalMatches
	}

	return &SearchResult{
		GeneratedResponse: generatedResponse,
		Total:             total,
		Elapsed:           time.Since(start),
		ChatModel:         s.cfg.ChatModel,
		SearchMethod:      "slack_only",
		Slack:             slackResult,
	}
}

// generateResponse uses LLM to generate a response based on Slack context
func (s *SlackOnlySearchAdapter) generateResponse(ctx context.Context, query string, contextParts []string) string {
	if s.chatClient == nil {
		// Try to create chat client if not provided
		if s.awsCfg == nil {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
			if err != nil {
				log.Printf("Failed to load AWS config: %v", err)
				return "回答の生成に失敗しました（AWS設定エラー）"
			}
			s.awsCfg = &cfg
		}
		s.chatClient = bedrock.GetSharedBedrockClient(*s.awsCfg, s.cfg.ChatModel)
	}

	// Create RAG instruction for Slack-only context
	ragInstruction := "以下の参考文献は、あなたの質問に関連するSlack会話から検索されたものです。" +
		"必ずこれらの参考文献の内容に基づいて回答してください。" +
		"一般的な知識ではなく、提供されたSlack会話の具体的な内容を優先して使用してください。"

	contextualPrompt := fmt.Sprintf("%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
		ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), query)

	messages := []bedrock.ChatMessage{
		{Role: "user", Content: contextualPrompt},
	}

	response, err := s.chatClient.GenerateChatResponse(ctx, messages)
	if err != nil {
		log.Printf("Chat generation error: %v", err)
		return "回答の生成中にエラーが発生しました。"
	}

	// Add Slack references to response
	return s.appendSlackReferences(response)
}

// appendSlackReferences adds Slack conversation references to the response
func (s *SlackOnlySearchAdapter) appendSlackReferences(response string) string {
	// Note: References are already included in the SearchResult.Slack field
	// This method can be used to add additional formatting if needed
	return response
}
