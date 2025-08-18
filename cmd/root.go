package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kiberag",
	Short: "Kibela API Gateway - CLI tool for exporting Kibela notes",
	Long: `Kibela API Gateway (kiberag) is a CLI tool that allows you to export all notes 
from Kibela using the GraphQL API and save them as markdown files.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(vectorizeCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(chatCmd)
}

func checkEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}
