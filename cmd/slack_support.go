package cmd

import (
	"context"
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
	if result == nil || len(result.EnrichedMessages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Slack Conversations:\n")
	for _, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		sb.WriteString(fmt.Sprintf("- #%s at %s: %s\n",
			channelName(orig.Channel),
			humanTimestamp(orig.Timestamp),
			strings.TrimSpace(orig.Text),
		))
		for _, reply := range msg.ThreadMessages {
			sb.WriteString(fmt.Sprintf("    • Reply at %s: %s\n",
				humanTimestamp(reply.Timestamp),
				strings.TrimSpace(reply.Text),
			))
		}
	}
	return sb.String()
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
