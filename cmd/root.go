package cmd

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/metrics"
)

var rootCmd = &cobra.Command{
	Use:   "ragent",
	Short: "RAGent - RAG system builder for Markdown documents",
	Long: `RAGent is a CLI tool for building a RAG (Retrieval-Augmented Generation) system 
from Markdown documents using hybrid search capabilities (BM25 + vector search) 
with Amazon S3 Vectors and OpenSearch.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if err := metrics.Init(); err != nil {
			log.Printf("Warning: metrics initialization failed: %v", err)
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if err := metrics.Close(); err != nil {
			log.Printf("Warning: metrics close failed: %v", err)
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(vectorizeCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(mcpServerCmd)
	rootCmd.AddCommand(webuiCmd)
}
