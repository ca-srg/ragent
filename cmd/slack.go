package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/slack-go/slack"
	"github.com/spf13/cobra"

	appcfg "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/slackbot"
	commontypes "github.com/ca-srg/ragent/internal/types"
)

var (
	slackContextSize int
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
		// Load slack config
		scfg, err := appcfg.LoadSlack()
		if err != nil {
			return fmt.Errorf("failed to load slack config: %w", err)
		}
		logger := log.New(os.Stdout, "slack-bot ", log.LstdFlags)

		// Slack client
		// For Socket Mode, the app-level token must be supplied to the client options
		var clientOpts []slack.Option
		if scfg.SocketMode && scfg.AppToken != "" {
			clientOpts = append(clientOpts, slack.OptionAppLevelToken(scfg.AppToken))
		}
		client := slack.New(scfg.BotToken, clientOpts...)

		// Choose search adapter (fallback removed): require OpenSearch
		if cfg.OpenSearchEndpoint == "" {
			return fmt.Errorf("OpenSearch is required for slack-bot: set OPENSEARCH_ENDPOINT and related settings")
		}

		// Override context size if provided via command line
		if slackContextSize > 0 {
			scfg.MaxResults = slackContextSize
		}

		adapter := slackbot.NewHybridSearchAdapter(cfg, scfg.MaxResults)
		processor := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, adapter, &slackbot.Formatter{})

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

	// attach command
	rootCmd.AddCommand(slackCmd)
}

// ensure unused import of commontypes is referenced to keep module tidy
var _ = commontypes.QueryVectorsResult{}
