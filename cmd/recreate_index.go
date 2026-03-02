package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/ingestion"
)

var recreateIndexCmd = &cobra.Command{
	Use:   "recreate-index",
	Short: "Recreate OpenSearch index with correct mapping",
	Long:  `Delete and recreate the OpenSearch index with the proper mapping for vector embeddings`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ingestion.RunRecreateIndex()
	},
}

func init() {
	rootCmd.AddCommand(recreateIndexCmd)
}
