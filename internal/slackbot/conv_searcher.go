package slackbot

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/slack-go/slack"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

type SlackConversationService interface {
	Search(
		ctx context.Context,
		query string,
		channels []string,
		opts slacksearch.SearchOptions,
	) (*slacksearch.SlackSearchResult, error)
}

type botSlackSearcher struct {
	service SlackConversationService
	client  *slack.Client
	cache   sync.Map
}

func BuildConvSearcher(cfg *appcfg.Config, client *slack.Client, logger *log.Logger) SlackConversationSearcher {
	awsCfg, err := bedrock.BuildBedrockAWSConfig(context.Background(), cfg.BedrockRegion, cfg.BedrockBearerToken)
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
	return NewBotSlackSearcher(slackService, client)
}

func NewBotSlackSearcher(service SlackConversationService, client *slack.Client) SlackConversationSearcher {
	if service == nil {
		return nil
	}
	return &botSlackSearcher{service: service, client: client}
}

func (b *botSlackSearcher) SearchConversations(ctx context.Context, query string, opts SearchOptions) (*SlackConversationResult, error) {
	if b.service == nil {
		return nil, nil
	}
	channels := b.channelFilter(opts.ChannelID)
	// Propagate the full SearchOptions (most importantly opts.ActionToken
	// surfaced from the originating Slack event) to the slacksearch service
	// so it can switch to assistant.search.context when available.
	result, err := b.service.Search(ctx, query, channels, opts)
	if err != nil {
		return nil, err
	}
	return ConvertSlackSearchResult(result), nil
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

func ConvertSlackSearchResult(src *slacksearch.SlackSearchResult) *SlackConversationResult {
	if src == nil {
		return nil
	}
	dst := &SlackConversationResult{
		IterationCount: src.IterationCount,
		TotalMatches:   src.TotalMatches,
		IsSufficient:   src.IsSufficient,
		MissingInfo:    append([]string{}, src.MissingInfo...),
		Messages:       make([]SlackConversationMessage, 0, len(src.EnrichedMessages)),
	}
	for _, msg := range src.EnrichedMessages {
		conv := SlackConversationMessage{
			Channel:   msg.OriginalMessage.Channel,
			Timestamp: msg.OriginalMessage.Timestamp,
			User:      msg.OriginalMessage.User,
			Username:  msg.OriginalMessage.Username,
			Text:      msg.OriginalMessage.Text,
			Permalink: msg.Permalink,
			Thread:    make([]SlackThreadMessage, 0, len(msg.ThreadMessages)),
		}
		for _, reply := range msg.ThreadMessages {
			conv.Thread = append(conv.Thread, SlackThreadMessage{
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
