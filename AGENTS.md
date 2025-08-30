# Repository Guidelines

## Project Structure & Module Organization
- `main.go`: Entrypoint calling the Cobra root command.
- `cmd/`: CLI subcommands — `export`, `vectorize`, `query`, `list`, `chat`, `recreate-index`.
- `internal/`: Implementation packages (`kibela`, `export`, `embedding/bedrock`, `s3vector`, `opensearch`, `vectorizer`, `types`, `config`, `scanner`, `metadata`).
- `markdown/`: Exported notes; primary input for `vectorize`.
- `doc/`: Configuration/design notes (e.g., S3 Vector, filters).

## Build, Test, and Development Commands
- `go mod tidy`: Sync dependencies.
- `go build -o mdrag`: Build the CLI.
- `go run main.go <command>`: Run locally (e.g., `go run main.go export`).
- `go test ./...`: Run tests (few exist; add package tests as you contribute).
- `go vet ./...`: Basic static checks.
Example (env inline):
`OPENSEARCH_ENDPOINT=https://... OPENSEARCH_INDEX=mdrag-documents AWS_S3_VECTOR_BUCKET=... AWS_S3_VECTOR_INDEX=... go run main.go vectorize --dry-run`

## Coding Style & Naming Conventions
- Language: Go ≥ 1.25. Format with `go fmt ./...`; prefer `go vet` before PRs.
- Packages: lower-case, short names. Exported identifiers in PascalCase; unexported in lowerCamelCase.
- Errors: wrap with `%w`; pass `context.Context` for IO/remote calls.
- CLI: add subcommands under `cmd/<name>.go` using Cobra (`Use`, `Short`, `RunE`).

## Testing Guidelines
- Framework: Go `testing`; `stretchr/testify` is available for assertions/mocks.
- Naming: files end with `_test.go`; functions `TestXxx`.
- Scope: focus on unit tests in `internal/*` (mock AWS/Bedrock/OpenSearch where possible). Run with `go test ./internal/...`.

## Commit & Pull Request Guidelines
- Commits: follow Conventional Commits (e.g., `feat:`, `fix:`, `docs:`, `refactor:`). Example: `refactor: simplify OpenSearch client retry`.
- PRs: use `.github/PULL_REQUEST_TEMPLATE.md`. Include overview, motivation, impact, tests, and doc updates. Link related issues; attach CLI output or screenshots when helpful.

## Security & Configuration Tips
- Do not commit secrets or `.env`. Use `direnv` or local env vars.
- Required env (typical): `AWS_S3_VECTOR_BUCKET`, `AWS_S3_VECTOR_INDEX`, `AWS_S3_REGION`, `OPENSEARCH_ENDPOINT`, `OPENSEARCH_INDEX`, `OPENSEARCH_REGION`.
- Bedrock chat and some embeddings use region `us-east-1` by default. For dev clusters with self-signed certs, `OPENSEARCH_INSECURE_SKIP_TLS=true` (development only).

