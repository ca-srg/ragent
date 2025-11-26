- [Features](#features)
# RAGent - RAG System Builder for Markdown Documents

**[日本語版 (Japanese) / 日本語 README](README_ja.md)**

RAGent is a CLI tool for building a RAG (Retrieval-Augmented Generation) system from markdown documents using hybrid search capabilities (BM25 + vector search) with Amazon S3 Vectors and OpenSearch.

## Table of Contents

- [Features](#features)
- [Slack Search Integration](#slack-search-integration)
- [Embedding-Agnostic RAG](#embedding-agnostic-rag)
- [Architecture Overview](#architecture-overview)
- [Prerequisites](#prerequisites)
- [Required Environment Variables](#required-environment-variables)
 - [Installation](#installation)
 - [Releases](#releases)
- [Commands](#commands)
  - [vectorize - Vectorization and S3 Storage](#1-vectorize---vectorization-and-s3-storage)
  - [query - Semantic Search](#2-query---semantic-search)
  - [list - List Vectors](#3-list---list-vectors)
  - [chat - Interactive RAG Chat](#4-chat---interactive-rag-chat)
  - [slack-bot - Slack Bot for RAG Search](#5-slack-bot---slack-bot-for-rag-search)
  - [mcp-server - MCP Server for Claude Desktop Integration](#6-mcp-server---mcp-server-for-claude-desktop-integration-new)
- [Development](#development)
- [Typical Workflow](#typical-workflow)
- [Troubleshooting](#troubleshooting)
- [OpenSearch RAG Configuration](#opensearch-rag-configuration)
- [Automated Setup (setup.sh)](#automated-setup-setupsh)
- [License](#license)
- [MCP Server Integration](#mcp-server-integration)
- [Contributing](#contributing)

## Features

- **Vectorization**: Convert markdown files to embeddings using Amazon Bedrock
- **S3 Vector Integration**: Store generated vectors in Amazon S3 Vectors
- **Hybrid Search**: Combined BM25 + vector search using OpenSearch
- **Slack Search Integration**: Blend document results with Slack conversations via an iterative enrichment pipeline
- **Semantic Search**: Semantic similarity search using S3 Vector Index
- **Interactive RAG Chat**: Chat interface with context-aware responses
- **Vector Management**: List vectors stored in S3
- **Embedding-Agnostic RAG**: Optional search path that answers directly from Slack without requiring pre-built vectors
- **MCP Server**: Model Context Protocol server for Claude Desktop integration
- **OIDC Authentication**: OpenID Connect authentication with multiple providers
- **IP-based Security**: IP address-based access control with configurable bypass ranges and audit logging
- **Dual Transport**: HTTP and Server-Sent Events (SSE) transport support

## Slack Search Integration

Slack search extends every retrieval workflow by streaming recent conversations, threads, and channel timelines directly from Slack’s Web API. When `SLACK_SEARCH_ENABLED=true`, the following behavior is enabled automatically:

- `query` exposes `--enable-slack-search` and `--slack-channels` flags to opt in per request.
- `chat` surfaces Slack context in addition to documents without requiring extra flags.
- `slack-bot` responds with combined document + Slack answers inside Block Kit messages.
- `mcp-server` adds `enable_slack_search` and `slack_channels` parameters to the `hybrid_search` tool.

Each Slack lookup performs iterative query refinement, merges timeline context from threads, and runs a sufficiency check before the final answer is generated. Results include permalinks so operators can pivot back to the original conversation instantly.

Quick example:

```bash
# Search across documents and Slack channels in one command
RAGent query -q "incident timeline" --enable-slack-search --slack-channels "prod-incident,devops"
```

## Embedding-Agnostic RAG

Slack search is intentionally embedding-agnostic: the system fetches live messages at query time instead of relying on pre-generated vectors. This provides several benefits:

- **Operational cost savings**: no additional vector storage or nightly re-indexing jobs are required for Slack data.
- **Real-time awareness**: answers incorporate messages posted seconds ago, keeping incident timelines and release notes fresh.
- **Time-series preservation**: the pipeline keeps message order and thread structure so analysts can replay discussions accurately.
- **Seamless fallbacks**: when Slack returns no hits the document-only hybrid search continues without interruption.

Slack-only output is still fused with document references, giving operators a single consolidated view while staying within compliance rules.

## Architecture Overview

The diagram below highlights how document and Slack pipelines converge before the answer is generated.

```mermaid
graph LR
    MD[Markdown Documents] -->|Vectorize| VE[Amazon S3 Vectors]
    VE --> HY[Hybrid Search Engine]
    OS[(Amazon OpenSearch)] --> HY
    SL[Slack Workspace] -->|Conversations API| SS[SlackSearch Service]
    SS --> HY
    HY --> CT[Context Builder]
    CT --> AN["Answer Generation (Claude, Bedrock Chat)"]
```

### Detailed Architecture Diagrams

#### Command Overview

RAGent provides five main commands, each serving a specific purpose in the RAG workflow:

```mermaid
flowchart LR
    User([User])

    User -->|1| Vectorize[vectorize<br/>Markdown → Vectors]
    User -->|2| Query[query<br/>Semantic Search]
    User -->|3| Chat[chat<br/>Interactive RAG]
    User -->|4| SlackBot[slack-bot<br/>Slack Integration]
    User -->|5| MCPServer[mcp-server<br/>Claude Desktop]

    Vectorize -->|Store| S3V[(S3 Vectors)]
    Vectorize -->|Index| OS[(OpenSearch)]

    Query --> Hybrid[Hybrid Search<br/>BM25 + Vector]
    Chat --> Hybrid
    SlackBot --> Hybrid
    MCPServer --> Hybrid

    Hybrid --> S3V
    Hybrid --> OS
    Hybrid -->|Optional| Slack[(Slack API)]

    Hybrid --> Results[Search Results]
    Results --> Answer[Generated Answer<br/>via Bedrock Claude]

    style Vectorize fill:#e1f5ff
    style Query fill:#fff4e1
    style Chat fill:#ffe1f5
    style SlackBot fill:#e1ffe1
    style MCPServer fill:#f5e1ff
```

#### Hybrid Search Flow

The hybrid search engine combines BM25 keyword matching with vector similarity search:

```mermaid
sequenceDiagram
    participant User
    participant CLI as CLI Command
    participant Bedrock as Amazon Bedrock
    participant OS as OpenSearch
    participant Slack as Slack API
    participant Engine as Hybrid Engine

    User->>CLI: query "検索クエリ"
    CLI->>Bedrock: GenerateEmbedding(query)
    Bedrock-->>CLI: Vector[1024]

    par BM25 Search
        CLI->>OS: BM25 Search (kuromoji tokenizer)
        OS-->>CLI: BM25 Results (200 docs)
    and Vector Search
        CLI->>OS: k-NN Search (cosine similarity)
        OS-->>CLI: Vector Results (100 docs)
    end

    CLI->>Engine: Fuse Results (RRF/weighted_sum)
    Engine-->>CLI: Fused Results (top-k)

    opt Slack Search Enabled
        CLI->>Slack: search.messages(query)
        Slack-->>CLI: Slack Messages
        CLI->>Slack: conversations.history(threads)
        Slack-->>CLI: Thread Context
        CLI->>Engine: Merge Slack Context
        Engine-->>CLI: Enriched Results
    end

    CLI-->>User: Combined Results + References
```

#### Slack Bot Processing

Slack Bot listens for mentions and responds with RAG-powered answers:

```mermaid
sequenceDiagram
    participant Slack as Slack Workspace
    participant Bot as Slack Bot (RTM/Socket)
    participant Detector as Mention Detector
    participant Extractor as Query Extractor
    participant Hybrid as Hybrid Search
    participant SlackSearch as Slack Search Service
    participant Formatter as Block Kit Formatter

    Slack->>Bot: @ragent-bot "質問内容"
    Bot->>Detector: Detect Mention
    Detector-->>Bot: Mention Event

    Bot->>Extractor: Extract Query
    Extractor-->>Bot: Query Text

    Bot->>Hybrid: Search(query)

    par Document Search
        Hybrid->>Hybrid: BM25 + k-NN
        Hybrid-->>Bot: Document Results
    and Slack Context Search (Optional)
        Bot->>SlackSearch: SearchSlack(query, channels)
        SlackSearch->>SlackSearch: Query Refinement Loop
        SlackSearch->>Slack: search.messages API
        Slack-->>SlackSearch: Messages
        SlackSearch->>Slack: conversations.history (threads)
        Slack-->>SlackSearch: Thread Timeline
        SlackSearch->>SlackSearch: Sufficiency Check
        SlackSearch-->>Bot: Slack Context
    end

    Bot->>Formatter: Format Results (Block Kit)
    Formatter-->>Bot: Slack Blocks

    Bot->>Slack: Post Message (with Blocks)
    Slack-->>Slack: Display Answer + References
```

#### MCP Server Integration

MCP Server exposes RAGent's hybrid search to Claude Desktop and other MCP-compatible tools:

```mermaid
sequenceDiagram
    participant Claude as Claude Desktop
    participant MCP as MCP Server
    participant Auth as Auth Middleware
    participant Handler as Hybrid Search Handler
    participant Bedrock as Amazon Bedrock
    participant OS as OpenSearch
    participant Slack as Slack API

    Claude->>MCP: JSON-RPC Request<br/>tools/call "hybrid_search"

    MCP->>Auth: Authenticate Request

    alt IP Authentication
        Auth->>Auth: Check Client IP
    else OIDC Authentication
        Auth->>Auth: Verify JWT Token
    else Bypass Range
        Auth->>Auth: Check Bypass CIDR
    end

    Auth-->>MCP: Authentication OK

    MCP->>Handler: Handle hybrid_search(query, options)

    Handler->>Bedrock: GenerateEmbedding(query)
    Bedrock-->>Handler: Vector[1024]

    par OpenSearch BM25
        Handler->>OS: BM25 Query
        OS-->>Handler: BM25 Results
    and OpenSearch k-NN
        Handler->>OS: k-NN Query
        OS-->>Handler: Vector Results
    end

    Handler->>Handler: Fuse Results (weighted_sum/RRF)

    opt enable_slack_search=true
        Handler->>Slack: Search Messages + Threads
        Slack-->>Handler: Slack Context
        Handler->>Handler: Merge Slack Results
    end

    Handler-->>MCP: Search Results (JSON)
    MCP-->>Claude: JSON-RPC Response<br/>{results, slack_results, metadata}
```

#### Vectorization Pipeline

The vectorize command processes markdown documents and stores them in dual backends:

```mermaid
flowchart TD
    Start([vectorize command]) --> CheckFlags{Check Flags}

    CheckFlags -->|--clear| Clear[Delete All Vectors<br/>+ Recreate Index]
    CheckFlags -->|Normal| Scan
    Clear --> Scan

    Scan[Scan Markdown Directory] --> Files[List .md Files]

    Files --> Loop{For Each File<br/>Parallel Processing}

    Loop --> Read[Read File Content]
    Read --> Extract[Extract Metadata<br/>FrontMatter Parser]
    Extract --> Embed[Generate Embedding<br/>Bedrock Titan v2]

    Embed --> Dual{Dual Backend Storage}

    Dual --> S3[Store to S3 Vector<br/>with Metadata]
    Dual --> OSIndex[Index to OpenSearch<br/>BM25 + k-NN fields]

    S3 --> Stats1[Update Stats<br/>S3: Success/Fail]
    OSIndex --> Stats2[Update Stats<br/>OS: Indexed/Skipped/Retry]

    Stats1 --> Check{More Files?}
    Stats2 --> Check

    Check -->|Yes| Loop
    Check -->|No| Report[Display Statistics<br/>Success Rate, Errors]

    Report --> Follow{Follow Mode?}
    Follow -->|Yes| Wait[Wait Interval<br/>Default: 30m]
    Wait --> Scan
    Follow -->|No| End([Completed])

    style Start fill:#e1f5ff
    style Clear fill:#ffe1e1
    style Embed fill:#fff4e1
    style Dual fill:#f5e1ff
    style End fill:#e1ffe1
```

## Prerequisites

### Prepare Markdown Documents

Before using RAGent, you need to prepare markdown documents in a `markdown/` directory. These documents should contain the content you want to make searchable through the RAG system.

```bash
# Create markdown directory
mkdir markdown

# Place your markdown files in this directory
cp /path/to/your/documents/*.md markdown/
```

For exporting notes from Kibela, use the separate export tool available in the `export/` directory.

## Required Environment Variables

Create a `.env` file in the project root and configure the following environment variables:

```env
# AWS Configuration
AWS_REGION=your_aws_region
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# S3 Vector Configuration
S3_VECTOR_INDEX_NAME=your_vector_index_name
S3_BUCKET_NAME=your_s3_bucket_name

# OpenSearch Configuration (for Hybrid RAG)
OPENSEARCH_ENDPOINT=your_opensearch_endpoint
OPENSEARCH_INDEX=your_opensearch_index
OPENSEARCH_REGION=us-east-1  # default

# Chat Configuration
CHAT_MODEL=anthropic.claude-3-5-sonnet-20240620-v1:0  # default
EXCLUDE_CATEGORIES=Personal,Daily  # Categories to exclude from search

# MCP Server Configuration
MCP_SERVER_HOST=localhost
MCP_SERVER_PORT=8080
MCP_IP_AUTH_ENABLED=true
MCP_ALLOWED_IPS=127.0.0.1,::1  # Comma-separated list

# MCP Bypass Configuration (optional)
MCP_BYPASS_IP_RANGE=10.0.0.0/8,172.16.0.0/12  # Comma-separated CIDR ranges
MCP_BYPASS_VERBOSE_LOG=false
MCP_BYPASS_AUDIT_LOG=true
MCP_TRUSTED_PROXIES=192.168.1.1,10.0.0.1  # Trusted proxy IPs for X-Forwarded-For

# OIDC Authentication (optional)
OIDC_ISSUER=https://accounts.google.com  # Your OIDC provider URL
OIDC_CLIENT_ID=your_client_id
OIDC_CLIENT_SECRET=your_client_secret

# Slack Bot Configuration
SLACK_BOT_TOKEN=xoxb-your-bot-token
SLACK_USER_TOKEN=xoxp-your-user-token-with-search-read
SLACK_RESPONSE_TIMEOUT=5s
SLACK_MAX_RESULTS=5
SLACK_ENABLE_THREADING=false
SLACK_THREAD_CONTEXT_ENABLED=true
SLACK_THREAD_CONTEXT_MAX_MESSAGES=10

# Slack Search Configuration
SLACK_SEARCH_ENABLED=false                     # Enable Slack search pipeline (set true to activate)
SLACK_SEARCH_MAX_RESULTS=20                    # Max Slack messages retrieved per query (1-100)
SLACK_SEARCH_MAX_RETRIES=5                     # Retry count for Slack API rate limits (0-10)
SLACK_SEARCH_CONTEXT_WINDOW_MINUTES=30         # Time window (minutes) of surrounding message context
SLACK_SEARCH_MAX_ITERATIONS=5                  # Iterative refinements performed when answers insufficient
SLACK_SEARCH_MAX_CONTEXT_MESSAGES=100          # Max messages assembled into enriched context
SLACK_SEARCH_TIMEOUT_SECONDS=60                 # Slack API request timeout in seconds (1-60)

# OpenTelemetry Configuration (optional)
OTEL_ENABLED=false
OTEL_SERVICE_NAME=ragent
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=service.namespace=ragent,environment=dev
OTEL_TRACES_SAMPLER=always_on
OTEL_TRACES_SAMPLER_ARG=1.0
```

Slack search requires `SLACK_SEARCH_ENABLED=true`, a valid `SLACK_BOT_TOKEN`, **and** a user token `SLACK_USER_TOKEN` that includes scopes such as `search:read`, `channels:history`, `groups:history`, and other conversation history scopes relevant to the channels you query. The search-specific knobs let you tune throughput and cost per workspace without touching the core document pipeline.

### MCP Bypass Configuration (Optional)

- `MCP_BYPASS_IP_RANGE`: Comma-separated CIDR ranges that skip authentication for trusted networks.
- `MCP_BYPASS_VERBOSE_LOG`: Enables detailed logging for bypass decisions to aid troubleshooting.
- `MCP_BYPASS_AUDIT_LOG`: Emits JSON audit entries for bypassed requests (enabled by default for compliance).
- `MCP_TRUSTED_PROXIES`: Comma-separated list of proxy IPs whose `X-Forwarded-For` headers are trusted during bypass checks.

## OpenTelemetry Observability

RAGent exposes distributed traces and usage metrics through the OpenTelemetry (OTel) Go SDK. Traces are emitted for Slack Bot message handling, MCP tool calls, and the shared hybrid search service, while counters and histograms capture request rates, error rates, and response times.

### Enable OTel

Set the following environment variables (see `.env.example` for defaults):

- `OTEL_ENABLED`: `true` to enable tracing and metrics (defaults to `false`).
- `OTEL_SERVICE_NAME`: Logical service name (`ragent` by default).
- `OTEL_RESOURCE_ATTRIBUTES`: Comma-separated `key=value` pairs such as `service.namespace=ragent,environment=dev`.
- `OTEL_EXPORTER_OTLP_ENDPOINT`: OTLP endpoint URL (including scheme).
- `OTEL_EXPORTER_OTLP_PROTOCOL`: `http/protobuf` (default) or `grpc`.
- `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`: Configure sampling strategy (`always_on`, `traceidratio`, etc.).

When `OTEL_ENABLED=false`, RAGent registers no-op providers and carries no runtime overhead.

### Example: Jaeger (local development)

```bash
# 1. Start Jaeger all-in-one
docker run --rm -it -p 4318:4318 -p 16686:16686 jaegertracing/all-in-one:1.58

# 2. Enable OTel before running RAGent
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf

# 3. Run the Slack bot (or any other command)
go run main.go slack-bot
# Visit http://localhost:16686 to explore spans
```

### Example: Prometheus via OpenTelemetry Collector

Use the OTel Collector to convert OTLP metrics into Prometheus format:

```yaml
# collector.yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
exporters:
  prometheus:
    endpoint: 0.0.0.0:9464
service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheus]
```

```bash
otelcol --config collector.yaml
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
go run main.go mcp-server
# Scrape metrics at http://localhost:9464/metrics
```

### Example: AWS X-Ray with ADOT Collector

```bash
# 1. Run the AWS Distro for OpenTelemetry collector
docker run --rm -it -p 4317:4317 public.ecr.aws/aws-observability/aws-otel-collector:latest

# 2. Configure RAGent to send spans over gRPC
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_TRACES_SAMPLER=traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.2
```

### Metrics & Span Names

- **Spans**
  - `slackbot.process_message`
  - `mcpserver.hybrid_search`
  - `search.hybrid`
- **Metrics**
  - `ragent.slack.requests.total`, `ragent.slack.errors.total`, `ragent.slack.response_time`
  - `ragent.mcp.requests.total`, `ragent.mcp.errors.total`, `ragent.mcp.response_time`

Attach additional attributes such as channel type, authentication method, tool name, and result totals for fine-grained analysis.

### Grafana Dashboard

RAGent includes a pre-configured Grafana dashboard template for visualizing OpenTelemetry metrics. The dashboard provides comprehensive monitoring of Slack Bot and MCP Server operations.

![Grafana Dashboard](assets/grafana.png)

**Dashboard Panels:**
- **Slack Metrics**
  - Slack Bot Requests (time series)
  - Slack Response Latency (histogram)
  - Slack Errors Rate (gauge)
- **MCP Metrics**
  - MCP Requests (time series)
  - MCP Response Time (histogram)
  - MCP Errors Rate (gauge)

**Setup Instructions:**

1. Enable OpenTelemetry in RAGent:
   ```bash
   export OTEL_ENABLED=true
   export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
   ```

2. Configure OTel Collector to export metrics to Prometheus (see example above)

3. Import the dashboard template into Grafana:
   ```bash
   # Using Grafana API
   curl -X POST http://localhost:3000/api/dashboards/import \
     -H "Content-Type: application/json" \
     -d @assets/grafana.json
   
   # Or manually import via Grafana UI
   # Dashboard > Import > Upload JSON file > Select assets/grafana.json
   ```

4. Configure Prometheus data source in Grafana pointing to your metrics endpoint

The dashboard will automatically start displaying metrics once RAGent begins processing requests.

## Installation

### Prerequisites

- Go 1.25.0 or higher
- direnv (recommended)

### Build

```bash
# Clone the repository
git clone https://github.com/ca-srg/ragent.git
cd RAGent

# Install dependencies
go mod download

# Build
go build -o RAGent

# Add executable to PATH (optional)
mv RAGent /usr/local/bin/
```

## Releases

Prebuilt binaries are published on GitHub Releases for Linux (amd64, arm64).

- Releases page: https://github.com/ca-srg/ragent/releases

### Create a Release (maintainers)

Use semantic versioning (`MAJOR.MINOR.PATCH`) and push a tag to trigger the pipeline:

```bash
# Example: v1.0.0
git tag v1.0.0
git push origin v1.0.0
```

This triggers the GitHub Actions workflow which builds archives via GoReleaser and publishes them to the release.

### Download Binaries

From the Releases page, download the archive for your platform:

- Linux amd64: `ragent_<VERSION>_linux_amd64.tar.gz`
- Linux arm64: `ragent_<VERSION>_linux_arm64.tar.gz`

Extract and place the binary on your `PATH`:

```bash
tar -xzf ragent_<VERSION>_linux_amd64.tar.gz
sudo mv ragent /usr/local/bin/
```

### Verify Checksums

Each release includes a SHA256 checksum file.

```bash
# Linux
sha256sum -c checksums_<VERSION>.txt

# macOS (if verifying from macOS)
shasum -a 256 -c checksums_<VERSION>.txt
```

All entries should report `OK`. If a mismatch occurs, re-download the artifact.

## Commands

### 1. vectorize - Vectorization and S3 Storage

Read markdown files, extract metadata, generate embeddings using Amazon Bedrock, and store them in Amazon S3 Vectors.

```bash
RAGent vectorize
```

**Options:**
- `-d, --directory`: Directory containing markdown files to process (default: `./markdown`)
- `--dry-run`: Display processing details without making actual API calls
- `-c, --concurrency`: Number of concurrent processes (0 = use default value from config file)

**Features:**
- Recursive scanning of markdown files
- Automatic metadata extraction
- Embedding generation using Amazon Titan Text Embedding v2
- Safe storage to S3 Vectors
- High-speed processing through concurrency

### 2. query - Semantic Search

Execute semantic similarity search against S3 Vector Index.

```bash
# Basic search
RAGent query -q "machine learning algorithms"

# Search with detailed options
RAGent query --query "API documentation" --top-k 5 --json

# Search with metadata filter
RAGent query -q "error handling" --filter '{"category":"programming"}'
```

**Options:**
- `-q, --query`: Search query text (required)
- `-k, --top-k`: Number of similar results to return (default: 10)
- `-j, --json`: Output results in JSON format
- `-f, --filter`: JSON metadata filter (e.g., `'{"category":"docs"}'`)
- `--enable-slack-search`: Include Slack conversations alongside document results when Slack search is enabled
- `--slack-channels`: Comma-separated channel names to scope Slack search (omit the leading '#')

**Usage Examples:**
```bash
# Search technical documentation
RAGent query -q "Docker container configuration" --top-k 3

# Search within specific category
RAGent query -q "authentication" --filter '{"type":"security"}' --json

# Get more results
RAGent query -q "database optimization" --top-k 20

# Merge Slack context for incident reviews
RAGent query -q "on-call handoff" --enable-slack-search --slack-channels "oncall,incident-review"
```

#### URL-Aware Search

RAGent inspects each query for HTTP/HTTPS URLs. When a URL is present, it first performs an exact term query on the `reference` field before running the usual hybrid search pipeline.

- Successful URL matches return immediately with `search_method` set to `"url_exact_match"` and include the URL-only results.
- If the term query fails or returns no hits, the engine falls back to the hybrid search flow and records the fallback reason (`term_query_error` or `term_query_no_results`).
- CLI `--json` output, Slack bot responses, and MCP tool results expose `search_method`, `url_detected`, and `fallback_reason` so callers can inspect how the result was produced.

```bash
RAGent query --json -q "https://example.com/doc のタイトルを教えて"
```

Example JSON response fragment:

```json
{
  "search_method": "url_exact_match",
  "url_detected": true,
  "fallback_reason": "",
  "results": [
    { "title": "Example Doc", "reference": "https://example.com/doc" }
  ]
}
```

If a URL is not detected or the exact match misses, `search_method` falls back to `"hybrid_search"` with the usual fused BM25/vector results.

### 3. list - List Vectors

Display a list of vectors stored in S3 Vector Index.

```bash
# Display all vectors
RAGent list

# Filter by prefix
RAGent list --prefix "docs/"
```

**Options:**
- `-p, --prefix`: Prefix to filter vector keys

**Features:**
- Display stored vector keys
- Filtering by prefix
- Check vector database contents

### 4. chat - Interactive RAG Chat

Start an interactive chat session using hybrid search (OpenSearch BM25 + vector search) for context retrieval and Amazon Bedrock (Claude) for generating responses.

```bash
# Start interactive chat with default settings
RAGent chat

# Chat with custom context size
RAGent chat --context-size 10

# Chat with custom weight balance for hybrid search
RAGent chat --bm25-weight 0.7 --vector-weight 0.3

# Chat with custom system prompt
RAGent chat --system "You are a helpful assistant specialized in documentation."

# Chat with Slack context enabled via environment flag
SLACK_SEARCH_ENABLED=true RAGent chat
```

**Options:**
- `-c, --context-size`: Number of context documents to retrieve (default: 5)
- `-i, --interactive`: Run in interactive mode (default: true)
- `-s, --system`: System prompt for the chat
- `-b, --bm25-weight`: Weight for BM25 scoring in hybrid search (0-1, default: 0.5)
- `-v, --vector-weight`: Weight for vector scoring in hybrid search (0-1, default: 0.5)
- `--use-japanese-nlp`: Use Japanese NLP optimization for OpenSearch (default: true)

When `SLACK_SEARCH_ENABLED=true`, chat sessions automatically pull in recent Slack conversations, show live progress for each refinement iteration, and append permalinks under the final answer.

**Features:**
- Hybrid search combining BM25 and vector similarity
- Context-aware responses using retrieved documents
- Conversation history management
- Reference citations with source links
- Japanese language optimization

**Chat Commands:**
- `exit` or `quit`: End the chat session
- `clear`: Clear conversation history
- `help`: Show available commands

### 5. slack-bot - Slack Bot for RAG Search

Start a Slack Bot that listens for mentions and answers with RAG results.

```bash
RAGent slack-bot
```

Requirements:
- Set `SLACK_BOT_TOKEN` in your environment (see `.env.example`).
- Invite the bot user to the target Slack channel.
- Optionally enable threading with `SLACK_ENABLE_THREADING=true`.
- Thread context: enable contextual search with `SLACK_THREAD_CONTEXT_ENABLED=true` (default) and control history depth via `SLACK_THREAD_CONTEXT_MAX_MESSAGES` (default `10` messages).
- Slack search: set `SLACK_SEARCH_ENABLED=true` to blend Slack conversations into every answer. Use `SLACK_SEARCH_MAX_RESULTS`, `SLACK_SEARCH_MAX_CONTEXT_MESSAGES`, and `SLACK_SEARCH_MAX_ITERATIONS` to tune throughput per workspace.
- Requires OpenSearch configuration (`OPENSEARCH_ENDPOINT`, `OPENSEARCH_INDEX`, `OPENSEARCH_REGION`). Slack Bot does not use S3 Vector fallback.

Details: see `docs/slack-bot.md`.

When enabled, the bot delivers a Block Kit section labelled **Conversations from Slack** with permalinks for each hit so responders can jump straight into the thread.

### 6. mcp-server - MCP Server for Claude Desktop Integration (New)

Start an MCP (Model Context Protocol) server that provides hybrid search capabilities to Claude Desktop and other MCP-compatible tools.

```bash
# Start with OIDC authentication only
RAGent mcp-server --auth-method oidc

# Allow either IP or OIDC authentication (recommended for development)
RAGent mcp-server --auth-method either

# Require both IP and OIDC authentication (highest security)
RAGent mcp-server --auth-method both

# IP authentication only (default)
RAGent mcp-server --auth-method ip
```

The `hybrid_search` MCP tool accepts two new parameters when the server is launched with `SLACK_SEARCH_ENABLED=true`:

- `enable_slack_search` (boolean, default `false`): opt in per request from your MCP client.
- `slack_channels` (string array): optional list of channel names to narrow the Slack search scope.

Responses include a `slack_results` array with message metadata and permalinks so downstream tools can render conversation context alongside document references.

**Authentication Methods:**
- `ip`: Traditional IP address-based authentication only
- `oidc`: OpenID Connect authentication only
- `both`: Requires both IP and OIDC authentication
- `either`: Allows either IP or OIDC authentication

**Bypass Authentication:**
For CI/CD environments and internal services, you can configure bypass IP ranges that skip authentication:
```bash
# Bypass authentication for specific IP ranges
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --bypass-ip-range "172.16.0.0/12"

# Enable audit logging for bypass access
RAGent mcp-server --bypass-ip-range "10.10.0.0/16" --bypass-audit-log

# Verbose logging for troubleshooting
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --bypass-verbose-log

# Configure trusted proxies for X-Forwarded-For
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --trusted-proxies "192.168.1.1"
```

**Supported OIDC Providers:**
- Google Workspace (`https://accounts.google.com`)
- Microsoft Azure AD/Entra ID (`https://login.microsoftonline.com/{tenant}/v2.0`)
- Okta (`https://{domain}.okta.com`)
- Keycloak (`https://{server}/realms/{realm}`)
- Custom OAuth2 providers

**Features:**
- JSON-RPC 2.0 compliant MCP protocol
- Hybrid search tool: `ragent-hybrid_search`
- Multiple authentication methods
- Claude Desktop integration
- SSE and HTTP transport support
- Browser-based authentication flow

**Requirements:**
- OpenSearch configuration is required for MCP server functionality
- For OIDC: Configure your OAuth2 application and set environment variables
- For IP auth: Configure allowed IP addresses or ranges

**Usage with Claude Desktop:**
After authentication, add the server to Claude Desktop using the provided command:
```bash
claude mcp add --transport sse ragent https://your-server.example.com/sse --header "Authorization: Bearer <JWT>"
```

Details: see `doc/mcp-server.md` and `doc/oidc-authentication.md`.

## Development

### Build Commands

```bash
# Format code
go fmt ./...

# Tidy dependencies
go mod tidy

# Run tests (if configured)
go test ./...

# Development execution
go run main.go [command]
```

### Project Structure

```
RAGent/
├── main.go                 # Entry point
├── cmd/                    # CLI command definitions
│   ├── root.go            # Root command and common settings
│   ├── query.go           # query command
│   ├── list.go            # list command
│   ├── chat.go            # chat command
│   ├── slack.go           # slack-bot command
│   ├── mcp-server.go      # mcp-server command (new)
│   └── vectorize.go       # vectorize command
├── internal/              # Internal libraries
│   ├── config/           # Configuration management
│   ├── embedding/        # Embedding generation
│   ├── s3vector/         # S3 Vector integration
│   ├── opensearch/       # OpenSearch integration
│   ├── vectorizer/       # Vectorization service
│   ├── slackbot/         # Slack Bot integration
│   └── mcpserver/        # MCP Server integration (new)
├── markdown/             # Markdown documents (prepare before use)
├── export/               # Separate export tool for Kibela
├── doc/                  # Project documentation
│   ├── mcp-server.md     # MCP Server setup guide
│   ├── oidc-authentication.md # OIDC authentication guide
│   ├── filter-configuration.md # Filter configuration guide
│   ├── s3-vector.md      # S3 Vector integration notes
│   └── score.md          # RAGスコアの基礎解説
├── .envrc                # direnv configuration
├── .env                  # Environment variables file
└── CLAUDE.md            # Claude Code configuration
```

## Dependencies

### Core Libraries

- **github.com/spf13/cobra**: CLI framework
- **github.com/joho/godotenv**: Environment variable loader
- **github.com/aws/aws-sdk-go-v2**: AWS SDK v2
  - S3 service
  - S3 Vectors
  - Bedrock Runtime (Titan Embeddings)
- **gopkg.in/yaml.v3**: YAML processing

### AWS Related Libraries

- `github.com/aws/aws-sdk-go-v2/config`: AWS configuration management
- `github.com/aws/aws-sdk-go-v2/service/s3`: S3 operations
- `github.com/aws/aws-sdk-go-v2/service/s3vectors`: S3 Vector operations
- `github.com/aws/aws-sdk-go-v2/service/bedrockruntime`: Bedrock Runtime operations

### MCP Integration

- `github.com/modelcontextprotocol/go-sdk`: Official MCP SDK v0.4.0
- `github.com/coreos/go-oidc`: OpenID Connect implementation
- JSON-RPC 2.0 protocol support
- Multiple authentication providers

## Typical Workflow

1. **Initial Setup**
   ```bash
   # Set environment variables
   cp .env.example .env
   # Edit .env file
   ```

2. **Prepare Markdown Documents**
   ```bash
   # Create markdown directory if not exists
   mkdir -p markdown
   
   # Place your markdown files in the directory
   # Or use the export tool for Kibela notes:
   cd export
   go build -o RAGent-export
   ./RAGent-export
   cd ..
   ```

3. **Vectorization and S3 Storage**
   ```bash
   # Verify with dry run
   RAGent vectorize --dry-run
   
   # Execute actual vectorization
   RAGent vectorize

   # Continuously vectorize using follow mode (default 30m interval)
   RAGent vectorize --follow

   # Customize the follow mode interval (e.g., every 15 minutes)
   RAGent vectorize --follow --interval 15m
   ```

   > Note: `--follow` cannot be combined with `--dry-run` or `--clear`.

4. **Check Vector Data**
   ```bash
   RAGent list
   ```

5. **Execute Semantic Search**
   ```bash
   RAGent query -q "content to search"

6. **Start MCP Server (for Claude Desktop)**
   ```bash
   # Configure OIDC provider (optional)
   export OIDC_ISSUER="https://accounts.google.com"
   export OIDC_CLIENT_ID="your-client-id"

   # Start MCP server
   RAGent mcp-server --auth-method either

   # Visit http://localhost:8080/login to authenticate
   # Follow instructions to add to Claude Desktop
   ```
   ```

## Troubleshooting

### Common Errors

1. **Environment variables not set**
   ```
   Error: required environment variable not set
   ```
   → Check if `.env` file is properly configured

2. **Configuration error**
   ```
   Error: configuration not found or invalid
   ```
   → Check configuration and authentication settings

3. **AWS authentication error**
   ```
   Error: AWS credentials not found
   ```
   → Check if AWS credentials are properly configured

4. **S3 Vector Index not found**
   ```
   Error: vector index not found
   ```
   → Verify S3 Vector Index is created

5. **MCP Server authentication error**
   ```
   Error: IP address not allowed: 192.168.1.100
   ```
   → Add IP to MCP_ALLOWED_IPS or use --auth-method oidc

6. **OIDC authentication error**
   ```
   Error: OIDC provider discovery failed
   ```
   → Check OIDC_ISSUER URL and network connectivity

7. **Slack token missing or invalid**
   ```
   Slack search unavailable: SLACK_BOT_TOKEN not configured
   ```
   → Ensure `SLACK_SEARCH_ENABLED=true` and `SLACK_BOT_TOKEN` belongs to the same workspace you are querying.

8. **Slack rate limit (HTTP 429)**
   ```
   slack search failed: rate_limited
   ```
   → Respect the `Retry-After` header, increase `SLACK_SEARCH_MAX_RETRIES`, or reduce `SLACK_SEARCH_MAX_RESULTS`.

9. **Bot not invited to channel**
   ```
   slack search failed: not_in_channel
   ```
   → Invite the bot user to the private channels you reference or remove them from `--slack-channels`.

### Debugging Methods

```bash
# Execute with detailed logs
RAGent vectorize --dry-run

# Check environment variables
env | grep AWS

# Test MCP server connectivity
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":"test"}' \
  http://localhost:8080/mcp
```

## OpenSearch RAG Configuration

### Role Mapping for AWS OpenSearch

When using AWS OpenSearch with IAM authentication, you need to configure role mapping to allow your IAM role to access the OpenSearch cluster.

#### Check Current Role Mapping
```bash
curl -u "master_user:master_pass" -X GET \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/all_access"
```

#### Map IAM Role to OpenSearch Role
```bash
curl -u "master_user:master_pass" -X PUT \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/all_access" \
  -H "Content-Type: application/json" \
  -d '{
    "backend_roles": ["arn:aws:iam::123456789012:role/your-iam-role"],
    "hosts": [],
    "users": []
  }'
```

#### Create Custom Role for RAG Operations
```bash
# Create a custom role with necessary permissions
curl -u "master_user:master_pass" -X PUT \
  "https://your-opensearch-endpoint/_plugins/_security/api/roles/RAGent_role" \
  -H "Content-Type: application/json" \
  -d '{
    "cluster_permissions": [
      "cluster:monitor/health",
      "indices:data/read/search"
    ],
    "index_permissions": [{
      "index_patterns": ["RAGent-*"],
      "allowed_actions": [
        "indices:data/read/search",
        "indices:data/read/get",
        "indices:data/write/index",
        "indices:data/write/bulk",
        "indices:admin/create",
        "indices:admin/mapping/put"
      ]
    }]
  }'

# Map IAM role to the custom role
curl -u "master_user:master_pass" -X PUT \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/RAGent_role" \
  -H "Content-Type: application/json" \
  -d '{
    "backend_roles": ["arn:aws:iam::123456789012:role/your-iam-role"],
    "hosts": [],
    "users": []
  }'
```

### Hybrid Search Configuration

For optimal RAG performance, configure hybrid search with appropriate weights:

- **General search**: BM25 weight: 0.5, Vector weight: 0.5
- **Keyword-focused**: BM25 weight: 0.7, Vector weight: 0.3
- **Semantic-focused**: BM25 weight: 0.3, Vector weight: 0.7

#### Recommended Settings for Japanese Documents
- BM25 Operator: "or" (default)
- BM25 Minimum Should Match: "2" or "70%" for precision
- Use Japanese NLP: true (enables kuromoji tokenizer)

## Automated Setup (setup.sh)

Use the interactive `setup.sh` to configure AWS OpenSearch security, create/mapping roles, create the target index, and grant IAM permissions for Bedrock and S3 Vectors. This script drives AWS CLI and signs OpenSearch Security API calls with SigV4.

Prerequisites
- AWS CLI v2 configured (credentials/profile with permission to update the domain and IAM)
- OpenSearch domain reachable (either VPC endpoint directly, or local port‑forward to `https://localhost:9200` with Host/SNI set to the VPC endpoint)

Run
```bash
bash setup.sh
```

What it asks
- AWS Account ID, OpenSearch domain/region, endpoint usage (direct vs. localhost:9200), IAM role ARNs (RAG runtime, optional master/admin), index name, S3 Vectors bucket/index/region, Bedrock region and model IDs.

What it does
- Updates the domain access policy to allow specified IAM roles
- (Optional) Sets AdvancedSecurity MasterUserARN
- Creates/updates OpenSearch role `kibela_rag_role` with cluster health + CRUD/bulk on <index>* and maps backend_roles to your IAM roles
- Creates the index if missing with Japanese analyzers and `knn_vector` (1024, lucene, cosinesimil)
- (Optional) Temporarily maps `all_access` to the RAG role for troubleshooting
- Adds IAM inline policies to the RAG role for Bedrock InvokeModel and S3 Vectors bucket/index operations

Notes
- If you use local port‑forwarding, the script sets the Host header to the VPC endpoint so SigV4 validation works against `https://localhost:9200`.
- The `all_access` mapping is optional and intended for short‑term troubleshooting; remove it after verification.

## License

For license information, please refer to the LICENSE file in the repository.

## MCP Server Integration

### Claude Desktop Configuration

After setting up the MCP server and completing authentication, add the server to your Claude Desktop configuration:

```json
{
  "mcpServers": {
    "ragent": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-H", "Authorization: Bearer YOUR_JWT_TOKEN",
        "-d", "@-",
        "http://localhost:8080/mcp"
      ],
      "env": {}
    }
  }
}
```

SSE clients (e.g., `claude mcp add --transport sse ...`) must target the dedicated `/sse` endpoint instead of `/mcp`.

### Available MCP Tools

- **ragent-hybrid_search**: Execute hybrid search using BM25 and vector search
  - Parameters: `query`, `max_results`, `bm25_weight`, `vector_weight`, `use_japanese_nlp`
  - Returns: Structured search results with fused scores (hybrid BM25/vector) and references
  - Score reference: see [doc/score.md](doc/score.md) for how the fused score is calculated and interpreted

### Authentication Flow

1. Start MCP server: `RAGent mcp-server --auth-method oidc`
2. Visit authentication URL: `http://localhost:8080/login`
3. Complete OAuth2 flow with your identity provider
4. Copy the provided Claude Desktop configuration
5. Add configuration to Claude Desktop settings

![OIDC Authentication](doc/oidc.png)

## Contributing

We welcome contributions to the project. Feel free to submit issues and pull requests.
