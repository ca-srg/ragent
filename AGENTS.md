# Agent Development Guidelines for ragent

## Build & Test Commands
- **Build**: `go build -o ragent` - Builds CLI binary
- **Test all**: `go test ./...` - Run all tests
- **Test single**: `go test -run TestName ./path/to/package` - Run specific test
- **Lint/Format**: `go fmt ./... && go vet ./...` - Format code and run static checks  
- **Dependencies**: `go mod tidy` - Clean up module dependencies

## Code Style
- **Go version**: ≥1.23, module path: `github.com/ca-srg/ragent`
- **Imports**: Group stdlib, then external, then internal packages. Use goimports
- **Naming**: PascalCase for exported, camelCase for unexported. Package names lowercase
- **Error handling**: Always wrap with `fmt.Errorf("%w", err)` for context. Never ignore errors
- **Context**: Pass `context.Context` as first parameter for IO/network operations
- **Testing**: Use `stretchr/testify` for assertions. Mock external services (AWS, OpenSearch)

## Project Structure
- `cmd/`: Cobra CLI subcommands (vectorize, query, chat, slack, etc.)
- `internal/`: Core packages - opensearch, vectorizer, slackbot, embedding, s3vector
- `tests/`: unit/, integration/, contract/ test organization

## Critical Rules
- NEVER commit `.env` or secrets. Use environment variables
- Run `go fmt` before any commit. Use `go vet` to catch issues
- Follow existing patterns in neighboring files. Check imports before adding new dependencies

