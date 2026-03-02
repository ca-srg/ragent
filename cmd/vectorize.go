package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/ingestion"
)

var (
	directory             string
	dryRun                bool
	concurrency           int
	clearVectors          bool
	followMode            bool
	followInterval        string
	spreadsheetConfigPath string
	csvConfigPath         string
	forceProcess          bool
	pruneDeleted          bool
	enableS3              bool
	s3Bucket              string
	s3Prefix              string
	s3VectorRegion        string
	s3SourceRegion        string
	githubRepos           string
)

var vectorizeCmd = &cobra.Command{
	Use:   "vectorize",
	Short: "Convert source files (markdown and CSV) to vectors and store in S3",
	Long: `
The vectorize command processes source files (markdown and CSV) in a directory,
extracts metadata, generates embeddings using Amazon Bedrock,
and stores the vectors in Amazon S3.

Supported file types:
  - Markdown (.md, .markdown): Each file becomes one document
  - CSV (.csv): Each row becomes one document (header row required)

This enables the creation of a vector database from your documentation
and data files for RAG (Retrieval Augmented Generation) applications.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ingestion.RunVectorize(cmd, ingestion.VectorizeOptions{
			Directory:             directory,
			DryRun:                dryRun,
			Concurrency:           concurrency,
			ClearVectors:          clearVectors,
			FollowMode:            followMode,
			FollowInterval:        followInterval,
			SpreadsheetConfigPath: spreadsheetConfigPath,
			CSVConfigPath:         csvConfigPath,
			ForceProcess:          forceProcess,
			PruneDeleted:          pruneDeleted,
			EnableS3:              enableS3,
			S3Bucket:              s3Bucket,
			S3Prefix:              s3Prefix,
			S3VectorRegion:        s3VectorRegion,
			S3SourceRegion:        s3SourceRegion,
			GitHubRepos:           githubRepos,
		})
	},
}

func init() {
	vectorizeCmd.Flags().StringVarP(&directory, "directory", "d", "./source", "Directory containing source files to process (markdown and CSV)")
	vectorizeCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be processed without making API calls")
	vectorizeCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "Number of concurrent operations (0 = use config default)")
	vectorizeCmd.Flags().BoolVar(&clearVectors, "clear", false, "Delete all existing vectors before processing new ones")
	vectorizeCmd.Flags().BoolVar(&followMode, "follow", false, "Continuously vectorize at a fixed interval")
	vectorizeCmd.Flags().StringVar(&followInterval, "interval", ingestion.DefaultFollowInterval, "Interval between vectorization runs in follow mode (e.g. 30m, 1h)")
	vectorizeCmd.Flags().StringVar(&spreadsheetConfigPath, "spreadsheet-config", "", "Path to spreadsheet configuration YAML file (enables spreadsheet mode)")
	vectorizeCmd.Flags().StringVar(&csvConfigPath, "csv-config", "", "Path to CSV configuration YAML file (for column mapping)")

	// Incremental processing options
	vectorizeCmd.Flags().BoolVarP(&forceProcess, "force", "f", false, "Force re-vectorization of all files, ignoring hash cache")
	vectorizeCmd.Flags().BoolVar(&pruneDeleted, "prune", false, "Remove vectors for files that no longer exist")

	// S3 source options
	vectorizeCmd.Flags().BoolVar(&enableS3, "enable-s3", false, "Enable S3 source file fetching")
	vectorizeCmd.Flags().StringVar(&s3Bucket, "s3-bucket", "", "S3 bucket name for source files (required when --enable-s3 is set)")
	vectorizeCmd.Flags().StringVar(&s3Prefix, "s3-prefix", "", "S3 prefix (directory) to scan (optional, defaults to bucket root)")

	// S3 region options
	vectorizeCmd.Flags().StringVar(&s3VectorRegion, "s3-vector-region", "", "AWS region for S3 Vector bucket (overrides S3_VECTOR_REGION, default: us-east-1)")
	vectorizeCmd.Flags().StringVar(&s3SourceRegion, "s3-source-region", "", "AWS region for source S3 bucket (overrides S3_SOURCE_REGION, default: us-east-1)")

	// GitHub source options
	vectorizeCmd.Flags().StringVar(&githubRepos, "github-repos", "", "Comma-separated list of GitHub repositories to clone and vectorize (format: owner/repo)")
}
