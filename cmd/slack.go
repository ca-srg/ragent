package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/slack-go/slack"
	"github.com/spf13/cobra"

	appcfg "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/observability"
	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/ca-srg/ragent/internal/slacksearch"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

var (
	slackContextSize int
	slackOnlyMode    bool
)

var slackCmd = &cobra.Command{
	Use:   "slack-bot",
	Short: "Start Slack Bot for RAG search",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load base config for search
		cfg, err := appcfg.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		logger := log.New(os.Stdout, "slack-bot ", log.LstdFlags)

		shutdown, obsErr := observability.Init(cfg)
		if obsErr != nil {
			logger.Printf("observability initialization error: %v", obsErr)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				logger.Printf("observability shutdown error: %v", err)
			}
		}()

		// Load slack config
		scfg, err := appcfg.LoadSlack()
		if err != nil {
			return fmt.Errorf("failed to load slack config: %w", err)
		}
		if strings.TrimSpace(cfg.SlackUserToken) == "" {
			return fmt.Errorf("slack user token (SLACK_USER_TOKEN) not configured; enable Slack search requires user token with search scopes")
		}

		// Slack client
		// For Socket Mode, the app-level token must be supplied to the client options
		var clientOpts []slack.Option
		if scfg.SocketMode && scfg.AppToken != "" {
			clientOpts = append(clientOpts, slack.OptionAppLevelToken(scfg.AppToken))
		}
		client := slack.New(scfg.BotToken, clientOpts...)

		// Choose search adapter (fallback removed): require OpenSearch unless --only-slack is used
		if !slackOnlyMode && cfg.OpenSearchEndpoint == "" {
			return fmt.Errorf("OpenSearch is required for slack-bot: set OPENSEARCH_ENDPOINT and related settings (use --only-slack to skip)")
		}

		// In --only-slack mode, force enable Slack search
		if slackOnlyMode {
			cfg.SlackSearchEnabled = true
			logger.Printf("Running in Slack-only mode (OpenSearch disabled)")
		}

		// Override context size if provided via command line
		if slackContextSize > 0 {
			scfg.MaxResults = slackContextSize
		}

		var convSearcher slackbot.SlackConversationSearcher
		awsCfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(cfg.S3VectorRegion))
		if err != nil {
			return fmt.Errorf("failed to load AWS config for Slack search: %w", err)
		}

		originalSlackEnabled := cfg.SlackSearchEnabled
		cfg.SlackSearchEnabled = true
		defer func() { cfg.SlackSearchEnabled = originalSlackEnabled }()

		if cfg.SlackSearchMaxResults <= 0 {
			cfg.SlackSearchMaxResults = scfg.MaxResults
		}
		if cfg.SlackSearchMaxContextMessages <= 0 {
			cfg.SlackSearchMaxContextMessages = scfg.MaxResults * 5
		}
		if cfg.SlackSearchTimeoutSeconds <= 0 {
			cfg.SlackSearchTimeoutSeconds = 60
		}

		bedrockClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)
		slackService, err := slacksearch.NewSlackSearchService(cfg, client, bedrockClient, logger)
		if err != nil {
			logger.Printf("slack search initialization failed; continuing without Slack search: %v", err)
		} else {
			if err := slackService.Initialize(context.Background()); err != nil {
				logger.Printf("slack search dependencies failed to initialize; continuing without Slack search: %v", err)
			} else {
				convSearcher = newBotSlackSearcher(slackService, client)
			}
		}

		// Choose adapter based on mode
		var adapter slackbot.SearchAdapter
		if slackOnlyMode {
			// Use Slack-only adapter
			chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)
			adapter = slackbot.NewSlackOnlySearchAdapter(cfg, scfg.MaxResults, convSearcher, chatClient, &awsCfg)
			logger.Printf("Using Slack-only search adapter")
		} else {
			// Use hybrid search adapter
			hybridAdapter := slackbot.NewHybridSearchAdapter(cfg, scfg.MaxResults, convSearcher, &awsCfg)
			hybridAdapter.SetSlackClient(client) // Enable Slack URL message fetching
			adapter = hybridAdapter
		}
		threadBuilder := slackbot.NewThreadContextBuilder(client, scfg, logger)
		processor := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, adapter, &slackbot.Formatter{}, threadBuilder)

		// Choose RTM vs Socket Mode
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if scfg.SocketMode && scfg.AppToken != "" {
			sbot, err := slackbot.NewSocketBot(client, scfg.AppToken, processor, logger)
			if err != nil {
				return err
			}
			sbot.SetEnableThreading(scfg.EnableThreading)
			sbot.SetRateLimiter(slackbot.NewRateLimiter(
				scfg.RateUserPerMinute,
				scfg.RateChannelPerMinute,
				scfg.RateGlobalPerMinute,
			))
			logger.Printf("Starting Slack Bot (Socket Mode) (max_results=%d)...", scfg.MaxResults)
			return sbot.Start(ctx)
		}

		bot, err := slackbot.NewBot(client, processor, logger)
		if err != nil {
			return err
		}
		// Configure options
		bot.SetEnableThreading(scfg.EnableThreading)
		bot.SetRateLimiter(slackbot.NewRateLimiter(
			scfg.RateUserPerMinute,
			scfg.RateChannelPerMinute,
			scfg.RateGlobalPerMinute,
		))

		// Run
		logger.Printf("Starting Slack Bot (RTM) (max_results=%d)...", scfg.MaxResults)
		return bot.Start(ctx)
	},
}

func init() {
	// add flags
	slackCmd.Flags().IntVarP(&slackContextSize, "context-size", "c", 0, "Number of context documents to retrieve (overrides config)")
	slackCmd.Flags().BoolVar(&slackOnlyMode, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")

	// attach command
	rootCmd.AddCommand(slackCmd)
}

// ensure unused import of commontypes is referenced to keep module tidy
var _ = commontypes.QueryVectorsResult{}

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
	channels := b.channelFilter(ctx, opts.ChannelID)
	result, err := b.service.Search(ctx, query, channels)
	if err != nil {
		return nil, err
	}
	return convertSlackSearchResult(result), nil
}

func (b *botSlackSearcher) channelFilter(ctx context.Context, channelID string) []string {
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
