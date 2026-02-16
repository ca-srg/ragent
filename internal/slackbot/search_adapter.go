package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
	"github.com/slack-go/slack"
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
	slackClient *slack.Client

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

// SetSlackClient sets the Slack client for URL message fetching
func (h *HybridSearchAdapter) SetSlackClient(client *slack.Client) {
	h.slackClient = client
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

	// Fetch messages from Slack URLs in the query (if any)
	var slackURLContext string
	if h.slackClient != nil {
		urls := detectSlackURLs(query)
		if len(urls) > 0 {
			log.Printf("Detected %d Slack URL(s) in query, fetching content...", len(urls))
			slackURLContext = fetchSlackURLMessages(ctx, h.slackClient, urls)
		}
	}

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
	filePathRefs := make(map[string]string)

	// Add Slack URL context first (highest priority - explicitly referenced by user)
	if slackURLContext != "" {
		contextParts = append(contextParts, slackURLContext)
	}

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

		if title != "" {
			if filePath, ok := source["file_path"].(string); ok && filePath != "" {
				filePathRefs[title] = convertGitHubPathToURL(filePath)
			}
		}
	}

	// Execute Slack search BEFORE LLM call to include Slack context in prompt
	var slackResult *SlackConversationResult
	if h.slackSearch != nil {
		var err error
		slackResult, err = h.slackSearch.SearchConversations(ctx, query, opts)
		if err != nil {
			log.Printf("slack search error: %v", err)
			slackResult = nil
		}
		// Add Slack context to prompt parts
		if slackResult != nil {
			if slackContext := slackResult.ForPrompt(); slackContext != "" {
				contextParts = append(contextParts, slackContext)
			}
		}
	}

	// Generate chat response using LLM (same as chat command)
	var generatedResponse string
	if len(contextParts) > 0 {
		// Create a strong instruction to use the retrieved context
		ragInstruction := "以下の参考文献は、あなたの質問に関連する社内ドキュメントやSlack会話から検索されたものです。" +
			"必ずこれらの参考文献の内容に基づいて回答してください。" +
			"一般的な知識ではなく、提供された参考文献の具体的な内容を優先して使用してください。" +
			"Slack会話が含まれている場合は、その内容も参照して回答してください。"

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

	if len(references) > 0 || len(filePathRefs) > 0 {
		allTitles := make(map[string]struct{})
		for t := range references {
			allTitles[t] = struct{}{}
		}
		for t := range filePathRefs {
			allTitles[t] = struct{}{}
		}

		var referenceBuilder strings.Builder
		referenceBuilder.WriteString(generatedResponse)
		referenceBuilder.WriteString("\n\n## 参考文献\n\n")

		for title := range allTitles {
			ref := references[title]
			fp := filePathRefs[title]
			switch {
			case ref != "" && fp != "":
				referenceBuilder.WriteString(fmt.Sprintf("- %s: %s (%s)\n", title, ref, fp))
			case ref != "":
				referenceBuilder.WriteString(fmt.Sprintf("- %s: %s\n", title, ref))
			case fp != "":
				referenceBuilder.WriteString(fmt.Sprintf("- %s: %s\n", title, fp))
			}
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

// slackURLInfo contains parsed Slack URL information
type slackURLInfo struct {
	ChannelID   string
	MessageTS   string
	ThreadTS    string
	OriginalURL string
}

// Slack URL pattern for archives
var slackArchiveURLPattern = regexp.MustCompile(
	`https?://[a-zA-Z0-9\-_.]+\.slack\.com/archives/([A-Z0-9]+)/p(\d{10})(\d{6})`,
)

// slackURLDetectPattern detects Slack URLs in text
var slackURLDetectPattern = regexp.MustCompile(
	`https?://[a-zA-Z0-9\-_.]+\.slack\.com/archives/[A-Z0-9]+/p\d{16}[^\s]*`,
)

// detectSlackURLs finds all Slack message URLs in text
func detectSlackURLs(text string) []*slackURLInfo {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	matches := slackURLDetectPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	results := make([]*slackURLInfo, 0, len(matches))

	for _, match := range matches {
		// Normalize URL
		normalized := strings.TrimRight(match, `>)]\'",.;:!?`)

		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		// Parse URL
		info, err := parseSlackURL(normalized)
		if err != nil {
			continue
		}
		results = append(results, info)
	}

	return results
}

// parseSlackURL extracts channel ID and timestamp from a Slack URL
func parseSlackURL(rawURL string) (*slackURLInfo, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if !strings.Contains(parsedURL.Host, "slack.com") {
		return nil, fmt.Errorf("not a Slack URL")
	}

	matches := slackArchiveURLPattern.FindStringSubmatch(rawURL)
	if len(matches) < 4 {
		return nil, fmt.Errorf("invalid Slack URL format")
	}

	info := &slackURLInfo{
		ChannelID:   matches[1],
		MessageTS:   fmt.Sprintf("%s.%s", matches[2], matches[3]),
		OriginalURL: rawURL,
	}

	// Extract thread_ts from query params
	if threadTS := parsedURL.Query().Get("thread_ts"); threadTS != "" {
		info.ThreadTS = threadTS
	}

	return info, nil
}

// fetchSlackURLMessages fetches messages from Slack URLs
func fetchSlackURLMessages(ctx context.Context, client *slack.Client, urls []*slackURLInfo) string {
	if client == nil || len(urls) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Referenced Slack Messages:\n")

	for i, info := range urls {
		if i >= 5 { // Limit to 5 URLs
			break
		}

		// Fetch message using conversation history
		params := &slack.GetConversationHistoryParameters{
			ChannelID: info.ChannelID,
			Oldest:    info.MessageTS,
			Latest:    info.MessageTS,
			Limit:     1,
			Inclusive: true,
		}

		history, err := client.GetConversationHistoryContext(ctx, params)
		if err != nil {
			log.Printf("Failed to fetch Slack message %s: %v", info.OriginalURL, err)
			continue
		}

		if len(history.Messages) == 0 {
			continue
		}

		msg := history.Messages[0]
		user := msg.User
		if msg.Username != "" {
			user = msg.Username
		}

		builder.WriteString(fmt.Sprintf("\n[%d] Channel: %s | User: %s\n", i+1, info.ChannelID, user))
		builder.WriteString(fmt.Sprintf("Message: %s\n", msg.Text))
		builder.WriteString(fmt.Sprintf("URL: %s\n", info.OriginalURL))

		// Fetch thread replies if applicable
		if info.ThreadTS != "" || msg.ThreadTimestamp != "" {
			threadTS := info.ThreadTS
			if threadTS == "" {
				threadTS = msg.ThreadTimestamp
			}

			repliesParams := &slack.GetConversationRepliesParameters{
				ChannelID: info.ChannelID,
				Timestamp: threadTS,
				Limit:     10,
			}

			replies, _, _, err := client.GetConversationRepliesContext(ctx, repliesParams)
			if err == nil && len(replies) > 1 {
				builder.WriteString(fmt.Sprintf("Thread Replies (%d):\n", len(replies)-1))
				for j, reply := range replies {
					if reply.Timestamp == msg.Timestamp {
						continue
					}
					if j > 5 {
						builder.WriteString("  ... (truncated)\n")
						break
					}
					replyUser := reply.User
					if reply.Username != "" {
						replyUser = reply.Username
					}
					builder.WriteString(fmt.Sprintf("  - %s: %s\n", replyUser, reply.Text))
				}
			}
		}
	}

	return builder.String()
}

func convertGitHubPathToURL(path string) string {
	const prefix = "github://"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	parts := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 3)
	if len(parts) < 3 {
		return path
	}
	return fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", parts[0], parts[1], parts[2])
}
