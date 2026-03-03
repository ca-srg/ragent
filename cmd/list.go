package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/ingestion"
)

var (
	prefix string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List vectors stored in S3 Vector Index",
	Long: `
List all vectors stored in the specified S3 Vector Index.
You can optionally provide a prefix to filter the results.

This command shows vector keys that have been stored in your S3 Vector Index,
helping you understand what data is available for querying.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ingestion.RunList(prefix)
	},
}

func init() {
	listCmd.Flags().StringVarP(&prefix, "prefix", "p", "", "Prefix to filter vector keys")
}
