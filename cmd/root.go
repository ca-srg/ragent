package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ragent",
	Short: "RAGent - RAG system builder for Markdown documents",
	Long: `RAGent is a CLI tool for building a RAG (Retrieval-Augmented Generation) system 
from Markdown documents using hybrid search capabilities (BM25 + vector search) 
with Amazon S3 Vectors and OpenSearch.`,
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
}
