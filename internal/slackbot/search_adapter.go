package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/mdrag/internal/embedding/bedrock"
	"github.com/ca-srg/mdrag/internal/opensearch"
	commontypes "github.com/ca-srg/mdrag/internal/types"
)

// SearchAdapter abstracts the RAG search for Slack
type SearchAdapter interface {
	Search(ctx context.Context, query string) *SearchResult
}

// HybridSearchAdapter uses OpenSearch Hybrid + Bedrock embedding
type HybridSearchAdapter struct {
	cfg        *commontypes.Config
	maxResults int
}

func NewHybridSearchAdapter(cfg *commontypes.Config, maxResults int) *HybridSearchAdapter {
	if maxResults <= 0 {
		maxResults = 5
	}
	return &HybridSearchAdapter{cfg: cfg, maxResults: maxResults}
}

func (h *HybridSearchAdapter) Search(ctx context.Context, query string) *SearchResult {
	start := time.Now()
	// AWS config fixed to us-east-1 for embedding and chat
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Printf("bedrock config error: %v", err)
		return &SearchResult{Items: nil, Total: 0, Elapsed: time.Since(start)}
	}
	embedClient := bedrock.NewBedrockClient(awsCfg, "amazon.titan-embed-text-v2:0")
	chatClient := bedrock.NewBedrockClient(awsCfg, h.cfg.ChatModel)

	// OpenSearch client
	osCfg, err := opensearch.NewConfigFromTypes(h.cfg)
	if err != nil {
		log.Printf("opensearch config error: %v", err)
		return &SearchResult{Items: nil, Total: 0, Elapsed: time.Since(start)}
	}
	osClient, err := opensearch.NewClient(osCfg)
	if err != nil {
		log.Printf("opensearch client error: %v", err)
		return &SearchResult{Items: nil, Total: 0, Elapsed: time.Since(start)}
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
		return &SearchResult{Items: nil, Total: 0, Elapsed: time.Since(start)}
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

	// Return the generated response as a single item
	return &SearchResult{
		GeneratedResponse: generatedResponse,
		Total:             len(res.FusionResult.Documents),
		Elapsed:           time.Since(start),
	}
}
