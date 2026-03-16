package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/slackbot"
)

var (
	slackContextSize    int
	slackOnlyMode       bool
	slackExportEval     bool
	slackExportEvalPath string
)

var slackCmd = &cobra.Command{
	Use:   "slack-bot",
	Short: "Start Slack Bot for RAG search",
	RunE: func(cmd *cobra.Command, args []string) error {
		return slackbot.RunSlackBot(context.Background(), slackbot.SlackBotOptions{
			ContextSize:    slackContextSize,
			OnlySlack:      slackOnlyMode,
			ExportEval:     slackExportEval,
			ExportEvalPath: slackExportEvalPath,
		})
	},
}

func init() {
	// add flags
	slackCmd.Flags().IntVarP(&slackContextSize, "context-size", "c", 0, "Number of context documents to retrieve (overrides config)")
	slackCmd.Flags().BoolVar(&slackOnlyMode, "only-slack", false, "Search only Slack conversations (skip OpenSearch)")
	slackCmd.Flags().BoolVar(&slackExportEval, "export-eval", false, "Enable evaluation data export")
	slackCmd.Flags().StringVar(&slackExportEvalPath, "export-eval-path", "./evaluation/exports/", "Output directory for JSONL evaluation data")

	// attach command
	rootCmd.AddCommand(slackCmd)
}
