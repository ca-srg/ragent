package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	appconfig "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/slacksearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
	"github.com/slack-go/slack"
)

var slackSearchRunner = performSlackSearch

func performSlackSearch(
	ctx context.Context,
	cfg *commontypes.Config,
	awsCfg aws.Config,
	embeddingClient opensearch.EmbeddingClient,
	userQuery string,
	channels []string,
	progressHandler func(iteration, max int),
) (*slacksearch.SlackSearchResult, error) {
	if !cfg.SlackSearchEnabled {
		return nil, fmt.Errorf("slack search disabled in configuration")
	}

	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot token (SLACK_BOT_TOKEN) not configured")
	}
	if strings.TrimSpace(cfg.SlackUserToken) == "" {
		return nil, fmt.Errorf("slack user token (SLACK_USER_TOKEN) not configured")
	}

	slackClient := slack.New(slackCfg.BotToken)

	chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)

	slackService, err := slacksearch.NewSlackSearchService(cfg, slackClient, chatClient, log.Default())
	if err != nil {
		return nil, err
	}
	if progressHandler != nil {
		slackService.SetProgressHandler(progressHandler)
	}
	if err := slackService.Initialize(ctx); err != nil {
		return nil, err
	}

	sanitizedChannels := sanitizeSlackChannels(channels)
	return slackService.Search(ctx, userQuery, sanitizedChannels)
}

func sanitizeSlackChannels(channels []string) []string {
	if len(channels) == 0 {
		return nil
	}
	clean := make([]string, 0, len(channels))
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		ch = strings.TrimPrefix(ch, "#")
		if ch != "" {
			clean = append(clean, ch)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func printSlackResults(result *slacksearch.SlackSearchResult) {
	fmt.Println("\n=== Slack Conversations ===")
	if result == nil || len(result.EnrichedMessages) == 0 {
		fmt.Println("  (no Slack messages found)")
		return
	}

	fmt.Printf("Iterations: %d | Total Matches: %d\n", result.IterationCount, result.TotalMatches)
	if len(result.Queries) > 0 {
		fmt.Printf("Queries tried: %s\n", strings.Join(result.Queries, ", "))
	}
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		fmt.Printf("Missing info: %s\n", strings.Join(result.MissingInfo, "; "))
	}

	for i, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n", i+1, channelName(orig.Channel), humanTimestamp(orig.Timestamp), displayUser(orig.User, orig.Username))
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     Permalink: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n", humanTimestamp(reply.Timestamp), strings.TrimSpace(reply.Text))
			}
		}
	}
}

func slackContextForPrompt(result *slacksearch.SlackSearchResult) string {
	return result.ForPrompt()
}

func channelName(id string) string {
	if id == "" {
		return "-"
	}
	return id
}

func displayUser(userID, username string) string {
	if username != "" {
		return username
	}
	if userID != "" {
		return userID
	}
	return "unknown"
}

func humanTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	seconds, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return ts
	}
	secs := int64(seconds)
	nsecs := int64((seconds - math.Floor(seconds)) * 1e9)
	return time.Unix(secs, nsecs).Format(time.RFC3339)
}

// fetchSlackURLContext detects Slack URLs in the query and fetches their content.
// Returns nil if no Slack URLs are found or if fetching fails.
// This function works independently of the Slack search enabled setting.
func fetchSlackURLContext(
	ctx context.Context,
	cfg *commontypes.Config,
	userQuery string,
) ([]slacksearch.EnrichedMessage, error) {
	// Check if query contains Slack URLs
	if !slacksearch.HasSlackURL(userQuery) {
		return nil, nil
	}

	// Load Slack configuration
	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN not configured for URL fetching")
	}

	// Create Slack client using bot token
	slackClient := slack.New(slackCfg.BotToken)

	// Create message fetcher with rate limiter
	fetcherConfig := &slacksearch.SlackSearchConfig{
		TimeoutSeconds: cfg.SlackSearchTimeoutSeconds,
	}
	if fetcherConfig.TimeoutSeconds <= 0 {
		fetcherConfig.TimeoutSeconds = 10
	}

	fetcher := slacksearch.NewMessageFetcher(slackClient, nil, fetcherConfig, log.Default())

	// Parse URLs from query
	urls := slacksearch.DetectSlackURLs(userQuery)
	if len(urls) == 0 {
		return nil, nil
	}

	log.Printf("Detected %d Slack URL(s) in query, fetching content...", len(urls))

	// Fetch messages
	response, err := fetcher.FetchByURLs(ctx, &slacksearch.FetchRequest{
		URLs:      urls,
		UserQuery: userQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Slack messages: %w", err)
	}

	if len(response.Errors) > 0 {
		log.Printf("Slack URL fetch warnings: %v", response.Errors)
	}

	return response.EnrichedMessages, nil
}

// printSlackURLContext prints fetched Slack URL context for CLI output.
func printSlackURLContext(messages []slacksearch.EnrichedMessage) {
	if len(messages) == 0 {
		return
	}

	fmt.Println("\n=== Referenced Slack Messages ===")
	for i, msg := range messages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n",
			i+1,
			channelName(orig.Channel),
			humanTimestamp(orig.Timestamp),
			displayUser(orig.User, orig.Username),
		)
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     URL: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n",
					humanTimestamp(reply.Timestamp),
					strings.TrimSpace(reply.Text),
				)
			}
		}
	}
}

// slackURLContextForPrompt formats fetched Slack URL messages for LLM prompt.
func slackURLContextForPrompt(messages []slacksearch.EnrichedMessage) string {
	return slacksearch.FormatFetchedContext(messages)
}

// performSlackOnlySearch executes Slack search without OpenSearch
// This is used in --only-slack mode
var performSlackOnlySearch = func(
	ctx context.Context,
	cfg *commontypes.Config,
	awsCfg aws.Config,
	userQuery string,
	channels []string,
	progressHandler func(iteration, max int),
) (*slacksearch.SlackSearchResult, error) {
	slackCfg, err := appconfig.LoadSlack()
	if err != nil {
		return nil, fmt.Errorf("failed to load Slack configuration: %w", err)
	}
	if slackCfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot token (SLACK_BOT_TOKEN) not configured")
	}
	if strings.TrimSpace(cfg.SlackUserToken) == "" {
		return nil, fmt.Errorf("slack user token (SLACK_USER_TOKEN) not configured")
	}

	slackClient := slack.New(slackCfg.BotToken)
	chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)

	// Force enable Slack search for this operation
	originalEnabled := cfg.SlackSearchEnabled
	cfg.SlackSearchEnabled = true
	defer func() { cfg.SlackSearchEnabled = originalEnabled }()

	slackService, err := slacksearch.NewSlackSearchService(cfg, slackClient, chatClient, log.Default())
	if err != nil {
		return nil, fmt.Errorf("failed to create Slack search service: %w", err)
	}
	if progressHandler != nil {
		slackService.SetProgressHandler(progressHandler)
	}
	if err := slackService.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize Slack search service: %w", err)
	}

	sanitizedChannels := sanitizeSlackChannels(channels)
	return slackService.Search(ctx, userQuery, sanitizedChannels)
}

// outputSlackOnlyResults outputs Slack search results for CLI
func outputSlackOnlyResults(result *slacksearch.SlackSearchResult, query string, outputJSON bool) error {
	if outputJSON {
		payload := struct {
			Query  string                         `json:"query"`
			Mode   string                         `json:"mode"`
			Result *slacksearch.SlackSearchResult `json:"result"`
		}{
			Query:  query,
			Mode:   "slack_only",
			Result: result,
		}
		jsonOutput, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	fmt.Printf("\nQuery: %s\n", query)
	fmt.Println("Search Type: slack_only")

	if result == nil {
		fmt.Println("  (no results)")
		return nil
	}

	fmt.Printf("Execution Time: %v\n", result.ExecutionTime)
	fmt.Printf("Iterations: %d | Total Matches: %d\n", result.IterationCount, result.TotalMatches)

	if len(result.Queries) > 0 {
		fmt.Printf("Queries tried: %s\n", strings.Join(result.Queries, ", "))
	}
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		fmt.Printf("Missing info: %s\n", strings.Join(result.MissingInfo, "; "))
	}

	if len(result.EnrichedMessages) == 0 {
		fmt.Println("\n  (no Slack messages found)")
		return nil
	}

	fmt.Println("\nResults:")
	for i, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n",
			i+1,
			channelName(orig.Channel),
			humanTimestamp(orig.Timestamp),
			displayUser(orig.User, orig.Username),
		)
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     Permalink: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n",
					humanTimestamp(reply.Timestamp),
					strings.TrimSpace(reply.Text),
				)
			}
		}
	}

	return nil
}
