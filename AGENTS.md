# AGENTS.md

Guidance for AI coding agents working with the RAGent codebase.

## Project Overview

RAGent is a Go CLI tool for building a Retrieval-Augmented Generation (RAG) system from Markdown/CSV documents. It uses hybrid search (BM25 + vector search) with Amazon S3 Vectors and OpenSearch.

## Build/Lint/Test Commands

```bash
# Build
go build -o RAGent

# Run without building
go run main.go <command>

# Format code (required before commit)
go fmt ./...

# Lint (standard)
go vet ./...

# Full lint (local development - requires golangci-lint)
make lint

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run single test by name
go test -v -run TestSearcherSearchSuccessWithFilters ./internal/slacksearch/...

# Run tests in specific package
go test ./internal/opensearch/...

# Run tests with coverage
go test -cover ./...

# Run benchmarks
go test -bench=. ./tests/benchmark/...

# Clean build cache
go clean -cache
```

## Code Style Guidelines

### Import Organization

Group imports in this order with blank lines between groups:

1. Standard library packages
2. Third-party packages
3. Internal project packages

```go
package example

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/slack-go/slack"

	"github.com/ca-srg/ragent/internal/types"
)
```

### Naming Conventions

- **Packages**: lowercase, short, no underscores (e.g., `slacksearch`, `opensearch`)
- **Exported types/functions**: PascalCase (e.g., `DocumentMetadata`, `NewClient`)
- **Unexported**: camelCase (e.g., `validateConfig`, `rateLimiter`)
- **Constants**: PascalCase for exported, camelCase for unexported
- **Interfaces**: Often suffixed with `-er` for single-method (e.g., `Searcher`, `Retriever`)
- **Error types**: Suffixed with `Error` (e.g., `SearchError`, `ProcessingError`)

### Type Definitions

Define types in dedicated files (`types.go`) within each package. Use struct tags for JSON and environment variables:

```go
type Config struct {
    Endpoint   string        `json:"endpoint" env:"OPENSEARCH_ENDPOINT,required=true"`
    RateLimit  float64       `json:"rate_limit" env:"OPENSEARCH_RATE_LIMIT,default=10.0"`
    Timeout    time.Duration `json:"timeout" env:"REQUEST_TIMEOUT,default=30s"`
}
```

### Error Handling

1. **Wrap errors with context** using `fmt.Errorf` and `%w`:

```go
if err != nil {
    return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
}
```

2. **Custom error types** for domain-specific errors with `Error()` and `IsRetryable()` methods

3. **Early returns** for error conditions - avoid deep nesting.

### Function Signatures

- Accept interfaces, return concrete types
- Use `context.Context` as first parameter for operations that may block
- Return `error` as the last return value

### Testing Patterns

Use `testify` for assertions and mocks. Test structure: Arrange/Act/Assert pattern.

### Concurrency

- Use `golang.org/x/sync/errgroup` for parallel operations
- Use `golang.org/x/time/rate` for rate limiting
- Always pass `context.Context` to allow cancellation

## Project Structure

```
cmd/              # CLI command definitions (cobra)
internal/
  config/         # Configuration loading and validation
  types/          # Shared type definitions
  opensearch/     # OpenSearch client and queries
  embedding/      # Embedding generation (Bedrock)
  slacksearch/    # Slack search pipeline
  slackbot/       # Slack Bot integration
  mcpserver/      # MCP Server implementation
  scanner/        # File scanning utilities
  filter/         # Search filter logic
```

## Documentation Rules

- Update both `README.md` and `README_ja.md` when changing documentation
- Do not use `go mod vendor` (vendoring is prohibited)
- Comments should explain "why", not "what"

## CLI Commands

```bash
./RAGent vectorize [--dry-run] [--follow]  # Vectorize documents
./RAGent query -q "query" [--top-k N]      # Semantic search
./RAGent chat                               # Interactive RAG chat
./RAGent list [--prefix "path/"]           # List vectors
./RAGent slack-bot                          # Start Slack Bot
./RAGent mcp-server                         # Start MCP Server
```

_Required read doc/_.md files for more information
