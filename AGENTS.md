# AGENTS.md — ragent

> RAGent: CLI tool for building RAG systems from Markdown documents using hybrid search (BM25 + vector) with Amazon S3 Vectors and OpenSearch.

## Build / Lint / Test Commands

```bash
# Build
go build -o ragent .

# Lint (full: fmt + vet + golangci-lint)
make lint

# Lint (CI subset: fmt + vet only)
make fmtvet

# Unit tests (excludes tests/ directory which contains integration/contract/benchmark)
go test $(go list ./... | grep -v '/tests/') -timeout 120s

# Single test function
go test -v -run TestFunctionName ./path/to/package/ -timeout 30s

# Single test with subtest
go test -v -run "TestParent/subtest_name" ./path/to/package/

# Unit tests in tests/unit/
go test -v ./tests/unit/... -timeout 60s

# Contract tests
go test -v ./tests/contract/... -timeout 60s

# Integration / E2E tests (requires OpenSearch via docker compose)
make test                # Full E2E: starts OpenSearch, creates index, runs tests
make test-teardown       # Cleanup after E2E

# E2E tests directly (OpenSearch must be running)
OPENSEARCH_ENDPOINT=http://localhost:9200 OPENSEARCH_INDEX=ragent-e2e-test \
  go test -v -run "TestE2E_" ./tests/integration/... -timeout 120s

# Benchmarks
go test -bench=. ./tests/benchmark/... -timeout 120s
```

## Architecture — Vertical Slice

This codebase follows a **vertical slice** architecture. Each feature slice lives under `internal/` and owns its full stack: types, interfaces, handlers, and tests.

```
main.go                        # Entrypoint → cmd.Execute()
cmd/                           # Cobra command definitions (thin wiring layer)
  root.go                      # Root command, subcommand registration
  vectorize.go, query.go, ...  # Each delegates to internal/<slice>
internal/
  ingestion/                   # Vectorization pipeline slice
    command.go                 #   Orchestrator (called from cmd/vectorize.go)
    vectorizer/                #   Core service, interfaces, error handling
    scanner/                   #   File scanning (local, S3, GitHub)
    metadata/                  #   Metadata extraction
    csv/, spreadsheet/, pdf/   #   Format-specific readers
    hashstore/                 #   Change detection for incremental processing
  mcpserver/                   # MCP server slice
    command.go                 #   Server bootstrap (called from cmd/mcp-server.go)
    server.go, server_wrapper.go
    hybrid_search_handler.go   #   Tool handler
    auth_middleware.go, oidc_auth.go, ip_auth.go
    types.go                   #   All MCP-specific types
  query/                       # Query/Chat slice
  slackbot/                    # Slack bot slice
  webui/                       # Web dashboard slice
  pkg/                         # Shared packages (cross-slice)
    config/                    #   Central config (env vars via netflix/go-env)
    domain/                    #   Shared interfaces & types
    embedding/                 #   Embedding clients (Bedrock, Gemini)
    opensearch/                #   OpenSearch client & hybrid engine
    s3vector/, sqlitevec/      #   Vector store backends
    slacksearch/               #   Slack search service
    metrics/, observability/   #   Telemetry
    ipc/                       #   Inter-process communication
    evalexport/                #   Eval export
tests/
  unit/                        # Unit tests (no external deps)
  contract/                    # Protocol/interface contract tests
  integration/                 # E2E tests (require OpenSearch, AWS)
  benchmark/, benchmarks/      # Performance benchmarks
  testdata/                    # Test fixtures (index mappings, etc.)
```

### Vertical Slice Rules

1. **Each slice owns its types.** Define domain types in the slice's `types.go`, not in `pkg/domain/`.
2. **Shared types go in `internal/pkg/domain/`** only when genuinely used across 2+ slices.
3. **`cmd/` is thin wiring.** Parse flags, build options struct, call `internal/<slice>.RunXxx(cmd, opts)`. No business logic.
4. **Inter-slice communication** via interfaces in `internal/pkg/domain/interfaces.go`.
5. **New features** → new directory under `internal/` or extend an existing slice. Never put business logic in `cmd/`.

## Code Style

### Imports (enforced by goimports)

Three groups separated by blank lines, ordered: stdlib → third-party → internal.

```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"
    "github.com/stretchr/testify/assert"

    appconfig "github.com/ca-srg/ragent/internal/pkg/config"
    "github.com/ca-srg/ragent/internal/pkg/domain"
)
```

Local prefix: `github.com/ca-srg/ragent` (configured in `.golangci-lint.yml`).

### Formatting & Line Length

- `gofmt` and `goimports` — enforced.
- Max line length: **140 characters** (enforced by `lll` linter).
- Tab width: 4 for line length calculation.

### Naming Conventions

- **Packages**: lowercase, single-word (`vectorizer`, `scanner`, `config`). No underscores.
- **Interfaces**: descriptive verbs/nouns (`EmbeddingClient`, `VectorStore`, `FileScanner`). No `I` prefix.
- **Structs**: PascalCase (`VectorizerService`, `SearchError`, `HybridSearchRequest`).
- **Constructors**: `NewXxx(...)` pattern (`NewFileScanner()`, `NewServerWrapper(cfg)`).
- **Config aliases**: use named imports for disambiguation: `appconfig "...internal/pkg/config"`, `appcfg`, `pkgconfig`.
- **Test functions**: `TestXxx` for units, `TestE2E_Xxx` for integration, `TestXxxCompliance` for contracts.
- **Test helpers in tests/**: use `requireEnv(t, key)` pattern to skip when env vars are missing.

### Error Handling

- **Always wrap with context**: `fmt.Errorf("failed to create vector store: %w", err)`.
- **Domain-specific error types**: each slice defines its own (e.g., `SearchError`, `ProcessingError`).
- **Error type classification**: `ErrorType` string constants in `internal/pkg/config/types.go`.
- **Constructor functions**: `NewSearchError(...)`, `NewProcessingError(...)`, `WrapError(...)`.
- **Retryable errors**: error types carry `Retryable bool` and `RetryAfter time.Duration`.
- **Never ignore errors**: `errcheck` linter is enabled with `check-type-assertions: true` and `check-blank: true`.
- **nolintlint**: any `//nolint` directive requires explanation and specific linter name.

### Testing Patterns

- Use `github.com/stretchr/testify` (`assert`, `require`) for assertions.
- **Table-driven tests** preferred for parameterized cases.
- **Test file placement**: co-located `_test.go` for unit tests in `internal/`; separated in `tests/` for integration/contract.
- **E2E test prefix**: `TestE2E_` — used by CI to filter integration tests.
- Test relaxations (see `.golangci-lint.yml`): `errcheck`, `gocognit`, `cyclop`, `lll` are disabled in `*_test.go`.
- **Test helpers** live in `_test_helpers.go` files (e.g., `cmd/output_test_helpers.go`).

### Configuration

- All config via environment variables parsed by `github.com/netflix/go-env` into `Config` struct.
- Struct tags: `env:"ENV_VAR_NAME,default=value"` or `env:"ENV_VAR_NAME,required=true"`.
- Load: `appconfig.Load()` → reads env → validates → returns `*Config`.
- Secrets: AWS Secrets Manager fallback via `LoadSecretsIntoEnv()`.

### Complexity Limits (enforced by linters)

- Cyclomatic complexity: max **12** per function (skip tests).
- Cognitive complexity: min trigger at **20**.
- Max naked return func lines: **80**.

### CI Checks

1. `go fmt ./...` + `go vet ./...`
2. `golangci-lint run` (26 linters enabled — see `.golangci-lint.yml`)
3. `nilaway ./...` (nil safety)
4. `zizmor` (GitHub Actions security — via pre-commit)
5. Unit tests: `go test $(go list ./... | grep -v '/tests/') -timeout 120s`
6. E2E tests: require OpenSearch (docker compose) + AWS credentials (OIDC)

## When environment variables are added, deleted, or updated

- Update @terraform/secret-template.json
- Update @.env.example
