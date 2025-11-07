package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

// SearchAdapter abstracts the RAG search for Slack
type SearchAdapter interface {
	Search(ctx context.Context, query string, opts SearchOptions) *SearchResult
}

type SearchOptions struct {
	ChannelID       string
	ThreadTimestamp string
}

// HybridSearchAdapter uses OpenSearch Hybrid + Bedrock embedding
type HybridSearchAdapter struct {
	cfg         *commontypes.Config
	maxResults  int
	slackSearch SlackConversationSearcher

	awsCfgMu sync.RWMutex
	awsCfg   *aws.Config
}

func NewHybridSearchAdapter(cfg *commontypes.Config, maxResults int, slackSearch SlackConversationSearcher, awsCfg *aws.Config) *HybridSearchAdapter {
	if maxResults <= 0 {
		maxResults = 5
	}

	adapter := &HybridSearchAdapter{cfg: cfg, maxResults: maxResults, slackSearch: slackSearch}
	if awsCfg != nil {
		adapter.awsCfg = awsCfg
	}
	return adapter
}

func (h *HybridSearchAdapter) awsConfig(ctx context.Context) (aws.Config, error) {
	h.awsCfgMu.RLock()
	if h.awsCfg != nil {
		cfg := *h.awsCfg
		h.awsCfgMu.RUnlock()
		return cfg, nil
	}
	h.awsCfgMu.RUnlock()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return aws.Config{}, err
	}

	h.awsCfgMu.Lock()
	defer h.awsCfgMu.Unlock()

	if h.awsCfg == nil {
		h.awsCfg = &cfg
		return cfg, nil
	}

	return *h.awsCfg, nil
}

func (h *HybridSearchAdapter) Search(ctx context.Context, query string, opts SearchOptions) *SearchResult {
	start := time.Now()
	awsCfg, err := h.awsConfig(ctx)
	if err != nil {
		log.Printf("bedrock config error: %v", err)
		return &SearchResult{
			Items:     nil,
			Total:     0,
			Elapsed:   time.Since(start),
			ChatModel: h.cfg.ChatModel,
		}
	}
	embedClient := bedrock.GetSharedBedrockClient(awsCfg, "amazon.titan-embed-text-v2:0")
	chatClient := bedrock.GetSharedBedrockClient(awsCfg, h.cfg.ChatModel)

	// OpenSearch client
	osCfg, err := opensearch.NewConfigFromTypes(h.cfg)
	if err != nil {
		log.Printf("opensearch config error: %v", err)
		return &SearchResult{
			Items:     nil,
			Total:     0,
			Elapsed:   time.Since(start),
			ChatModel: h.cfg.ChatModel,
		}
	}
	osClient, err := opensearch.NewClient(osCfg)
	if err != nil {
		log.Printf("opensearch client error: %v", err)
		return &SearchResult{
			Items:     nil,
			Total:     0,
			Elapsed:   time.Since(start),
			ChatModel: h.cfg.ChatModel,
		}
	}

	engine := opensearch.NewHybridSearchEngine(osClient, embedClient)
	res, err := engine.Search(ctx, &opensearch.HybridQuery{
		Query:          query,
		IndexName:      h.cfg.OpenSearchIndex,
		Size:           h.maxResults,
		BM25Weight:     0.5,
		VectorWeight:   0.5,
		FusionMethod:   opensearch.FusionMethodRRF,
		UseJapaneseNLP: true,
		TimeoutSeconds: 10,
	})
	if err != nil || res == nil || res.FusionResult == nil {
		log.Printf("hybrid search failed: %v", err)
		return &SearchResult{
			Items:     nil,
			Total:     0,
			Elapsed:   time.Since(start),
			ChatModel: h.cfg.ChatModel,
		}
	}

	// Extract context and references from results (same as chat command)
	var contextParts []string
	references := make(map[string]string)

	for _, doc := range res.FusionResult.Documents {
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

	// Generate chat response using LLM (same as chat command)
	var generatedResponse string
	if len(contextParts) > 0 {
		// Create a strong instruction to use the retrieved context
		ragInstruction := "以下の参考文献は、あなたの質問に関連する社内ドキュメントから検索されたものです。" +
			"必ずこれらの参考文献の内容に基づいて回答してください。" +
			"一般的な知識ではなく、提供された参考文献の具体的な内容を優先して使用してください。"

		contextualPrompt := fmt.Sprintf("%s\n\n参考文献:\n%s\n\nユーザーの質問: %s",
			ragInstruction, strings.Join(contextParts, "\n\n---\n\n"), query)

		messages := []bedrock.ChatMessage{
			{Role: "user", Content: contextualPrompt},
		}

		// Generate response
		response, err := chatClient.GenerateChatResponse(ctx, messages)
		if err != nil {
			log.Printf("chat generation error: %v", err)
			generatedResponse = "回答の生成中にエラーが発生しました。"
		} else {
			generatedResponse = response
		}
	} else {
		generatedResponse = "関連する情報が見つかりませんでした。"
	}

	var slackResult *SlackConversationResult
	if h.slackSearch != nil {
		var err error
		slackResult, err = h.slackSearch.SearchConversations(ctx, query, opts)
		if err != nil {
			log.Printf("slack search error: %v", err)
			slackResult = nil
		}
	}

	// Add references to response if any were found
	if len(references) > 0 {
		var referenceBuilder strings.Builder
		referenceBuilder.WriteString(generatedResponse)
		referenceBuilder.WriteString("\n\n## 参考文献\n\n")

		// Display title: reference format
		for title, ref := range references {
			referenceBuilder.WriteString(fmt.Sprintf("- %s: %s\n", title, ref))
		}

		generatedResponse = referenceBuilder.String()
	}

	total := res.FusionResult.TotalHits
	if slackResult != nil {
		total += slackResult.TotalMatches
	}

	// Return the generated response as a single item
	return &SearchResult{
		GeneratedResponse: generatedResponse,
		Total:             total,
		Elapsed:           time.Since(start),
		ChatModel:         h.cfg.ChatModel,
		SearchMethod:      res.SearchMethod,
		URLDetected:       res.URLDetected,
		FallbackReason:    res.FallbackReason,
		Slack:             slackResult,
	}
}
