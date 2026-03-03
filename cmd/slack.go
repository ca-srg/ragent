package cmd

import (
	"context"
	"log"
	"strings"
	"sync"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/slack-go/slack"
	"github.com/spf13/cobra"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
	"github.com/ca-srg/ragent/internal/slackbot"
)

var (
	slackContextSize int
	slackOnlyMode    bool
)

var slackCmd = &cobra.Command{
	Use:   "slack-bot",
	Short: "Start Slack Bot for RAG search",
	RunE: func(cmd *cobra.Command, args []string) error {
		return slackbot.RunSlackBot(context.Background(), slackbot.SlackBotOptions{
			ContextSize:       slackContextSize,
			OnlySlack:         slackOnlyMode,
			BuildConvSearcher: buildSlackConvSearcher,
		})
	},
}

func init() {
	// add flags
	slackCmd.Flags().IntVarP(&slackContextSize, "context-size", "c", 0, "Number of context documents to retrieve (overrides config)")
	slackCmd.Flags().BoolVar(&slackOnlyMode, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")

	// attach command
	rootCmd.AddCommand(slackCmd)
}

// buildSlackConvSearcher creates a SlackConversationSearcher backed by slacksearch.
// It lives in cmd/ to avoid the internal/slackbot ↔ internal/pkg/slacksearch circular import.
func buildSlackConvSearcher(cfg *appcfg.Config, client *slack.Client, logger *log.Logger) slackbot.SlackConversationSearcher {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.S3VectorRegion))
	if err != nil {
		logger.Printf("failed to load AWS config for slack search: %v", err)
		return nil
	}
	bedrockClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)
	slackService, err := slacksearch.NewSlackSearchService(cfg, client, bedrockClient, logger)
	if err != nil {
		logger.Printf("slack search initialization failed; continuing without Slack search: %v", err)
		return nil
	}
	if err := slackService.Initialize(context.Background()); err != nil {
		logger.Printf("slack search dependencies failed to initialize; continuing without Slack search: %v", err)
		return nil
	}
	return newBotSlackSearcher(slackService, client)
}

// slackConversationService is the narrow interface we need from slacksearch.SlackSearchService.
// Keeping it here avoids importing slacksearch from internal/slackbot (would be circular).
type slackConversationService interface {
	Search(ctx context.Context, query string, channels []string) (*slacksearch.SlackSearchResult, error)
}

type botSlackSearcher struct {
	service slackConversationService
	client  *slack.Client
	cache   sync.Map
}

func newBotSlackSearcher(service slackConversationService, client *slack.Client) slackbot.SlackConversationSearcher {
	if service == nil {
		return nil
	}
	return &botSlackSearcher{service: service, client: client}
}

func (b *botSlackSearcher) SearchConversations(ctx context.Context, query string, opts slackbot.SearchOptions) (*slackbot.SlackConversationResult, error) {
	if b.service == nil {
		return nil, nil
	}
	channels := b.channelFilter(opts.ChannelID)
	result, err := b.service.Search(ctx, query, channels)
	if err != nil {
		return nil, err
	}
	return convertSlackSearchResult(result), nil
}

func (b *botSlackSearcher) channelFilter(channelID string) []string {
	if channelID == "" || strings.HasPrefix(channelID, "D") {
		return nil
	}
	if name, ok := b.cache.Load(channelID); ok {
		if str, ok2 := name.(string); ok2 && str != "" {
			return []string{str}
		}
	}
	if b.client == nil {
		return nil
	}
	info, err := b.client.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: channelID})
	if err != nil || info == nil || info.Name == "" {
		if err != nil {
			log.Printf("slack conversation info error: %v", err)
		}
		return nil
	}
	b.cache.Store(channelID, info.Name)
	return []string{info.Name}
}

func convertSlackSearchResult(src *slacksearch.SlackSearchResult) *slackbot.SlackConversationResult {
	if src == nil {
		return nil
	}
	dst := &slackbot.SlackConversationResult{
		IterationCount: src.IterationCount,
		TotalMatches:   src.TotalMatches,
		IsSufficient:   src.IsSufficient,
		MissingInfo:    append([]string{}, src.MissingInfo...),
		Messages:       make([]slackbot.SlackConversationMessage, 0, len(src.EnrichedMessages)),
	}
	for _, msg := range src.EnrichedMessages {
		conv := slackbot.SlackConversationMessage{
			Channel:   msg.OriginalMessage.Channel,
			Timestamp: msg.OriginalMessage.Timestamp,
			User:      msg.OriginalMessage.User,
			Username:  msg.OriginalMessage.Username,
			Text:      msg.OriginalMessage.Text,
			Permalink: msg.Permalink,
			Thread:    make([]slackbot.SlackThreadMessage, 0, len(msg.ThreadMessages)),
		}
		for _, reply := range msg.ThreadMessages {
			conv.Thread = append(conv.Thread, slackbot.SlackThreadMessage{
				Timestamp: reply.Timestamp,
				User:      reply.User,
				Username:  reply.Username,
				Text:      reply.Text,
			})
		}
		dst.Messages = append(dst.Messages, conv)
	}
	return dst
}
