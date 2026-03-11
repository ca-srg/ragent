package slackbot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/slack-go/slack"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/evalexport"
	"github.com/ca-srg/ragent/internal/pkg/metrics"
	"github.com/ca-srg/ragent/internal/pkg/observability"
)

// SlackBotOptions holds the command-line options for the slack-bot command.
// BuildConvSearcher is provided by the cmd layer to avoid the
// internal/slackbot ↔ internal/pkg/slacksearch circular import.
type SlackBotOptions struct {
	ContextSize       int
	OnlySlack         bool
	ExportEval        bool
	ExportEvalPath    string
	BuildConvSearcher func(cfg *appcfg.Config, client *slack.Client, logger *log.Logger) SlackConversationSearcher
}

// RunSlackBot initializes and starts the Slack Bot with the given options.
func RunSlackBot(ctx context.Context, opts SlackBotOptions) error {
	metrics.RecordInvocation(metrics.ModeSlack)

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
	if err := metrics.InitOTelMetrics(); err != nil {
		logger.Printf("metrics OTel initialization error: %v", err)
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
	if !opts.OnlySlack && cfg.OpenSearchEndpoint == "" {
		return fmt.Errorf("OpenSearch is required for slack-bot: set OPENSEARCH_ENDPOINT and related settings (use --only-slack to skip)")
	}

	// In --only-slack mode, force enable Slack search
	if opts.OnlySlack {
		cfg.SlackSearchEnabled = true
		logger.Printf("Running in Slack-only mode (OpenSearch disabled)")
	}

	// Override context size if provided via command line
	if opts.ContextSize > 0 {
		scfg.MaxResults = opts.ContextSize
	}

	awsCfg, err := bedrock.BuildBedrockAWSConfig(ctx, cfg.BedrockRegion, cfg.BedrockBearerToken)
	if err != nil {
		return fmt.Errorf("failed to load Bedrock AWS config for Slack search: %w", err)
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

	// Build the conversation searcher via the factory provided by cmd layer.
	// This avoids importing internal/pkg/slacksearch from internal/slackbot (circular dep).
	var convSearcher SlackConversationSearcher
	if opts.BuildConvSearcher != nil {
		convSearcher = opts.BuildConvSearcher(cfg, client, logger)
	}

	// Choose adapter based on mode
	var adapter SearchAdapter
	if opts.OnlySlack {
		// Use Slack-only adapter
		chatClient := bedrock.GetSharedBedrockClient(awsCfg, cfg.ChatModel)
		adapter = NewSlackOnlySearchAdapter(cfg, scfg.MaxResults, convSearcher, chatClient, &awsCfg)
		logger.Printf("Using Slack-only search adapter")
	} else {
		// Use hybrid search adapter
		hybridAdapter := NewHybridSearchAdapter(cfg, scfg.MaxResults, convSearcher, &awsCfg)
		hybridAdapter.SetSlackClient(client) // Enable Slack URL message fetching
		adapter = hybridAdapter
	}

	if opts.ExportEval {
		evalWriter, werr := evalexport.NewWriter(opts.ExportEvalPath)
		if werr != nil {
			logger.Printf("Warning: failed to create eval export writer: %v", werr)
		} else {
			if hybridAdapter, ok := adapter.(*HybridSearchAdapter); ok {
				hybridAdapter.SetEvalWriter(evalWriter)
			}
			if slackOnlyAdapter, ok := adapter.(*SlackOnlySearchAdapter); ok {
				slackOnlyAdapter.SetEvalWriter(evalWriter)
			}
		}
	}

	threadBuilder := NewThreadContextBuilder(client, scfg, logger)
	processor := NewProcessor(&MentionDetector{}, &QueryExtractor{}, adapter, &Formatter{}, threadBuilder)

	// Choose RTM vs Socket Mode
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if scfg.SocketMode && scfg.AppToken != "" {
		sbot, err := NewSocketBot(client, scfg.AppToken, processor, logger)
		if err != nil {
			return err
		}
		sbot.SetEnableThreading(scfg.EnableThreading)
		sbot.SetRateLimiter(NewRateLimiter(
			scfg.RateUserPerMinute,
			scfg.RateChannelPerMinute,
			scfg.RateGlobalPerMinute,
		))
		logger.Printf("Starting Slack Bot (Socket Mode) (max_results=%d)...", scfg.MaxResults)
		return sbot.Start(ctx)
	}

	bot, err := NewBot(client, processor, logger)
	if err != nil {
		return err
	}
	// Configure options
	bot.SetEnableThreading(scfg.EnableThreading)
	bot.SetRateLimiter(NewRateLimiter(
		scfg.RateUserPerMinute,
		scfg.RateChannelPerMinute,
		scfg.RateGlobalPerMinute,
	))

	// Run
	logger.Printf("Starting Slack Bot (RTM) (max_results=%d)...", scfg.MaxResults)
	return bot.Start(ctx)
}
