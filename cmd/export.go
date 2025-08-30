package cmd

import (
	"fmt"
	"os"

	"github.com/ca-srg/kiberag/internal/config"
	"github.com/ca-srg/kiberag/internal/export"
	"github.com/ca-srg/kiberag/internal/kibela"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all notes from Kibela to markdown files",
	Long:  `Export all notes from Kibela using GraphQL API and save them as markdown files in the markdown/ directory.`,
	RunE:  runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	client := kibela.NewClient(cfg.KibelaTeam, cfg.KibelaToken)

	if err := os.MkdirAll("markdown", 0755); err != nil {
		return fmt.Errorf("failed to create markdown directory: %w", err)
	}

	exporter := export.New(client)

	fmt.Println("Starting export of all Kibela notes...")

	err = exporter.ExportAllNotes("markdown")
	if err != nil {
		return fmt.Errorf("failed to export notes: %w", err)
	}

	fmt.Println("Export completed successfully!")
	return nil
}
