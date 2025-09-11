# mdRAG - RAG System Builder for Markdown Documents

**[日本語版 (Japanese) / 日本語 README](README_ja.md)**

mdRAG is a CLI tool for building a RAG (Retrieval-Augmented Generation) system from markdown documents using hybrid search capabilities (BM25 + vector search) with Amazon S3 Vectors and OpenSearch.

## Features

- **Vectorization**: Convert markdown files to embeddings using Amazon Bedrock
- **S3 Vector Integration**: Store generated vectors in Amazon S3 Vectors
- **Hybrid Search**: Combined BM25 + vector search using OpenSearch
- **Semantic Search**: Semantic similarity search using S3 Vector Index
- **Interactive RAG Chat**: Chat interface with context-aware responses
- **Vector Management**: List vectors stored in S3

## Prerequisites

### Prepare Markdown Documents

Before using mdRAG, you need to prepare markdown documents in a `markdown/` directory. These documents should contain the content you want to make searchable through the RAG system.

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
```

## Installation

### Prerequisites

- Go 1.25.0 or higher
- direnv (recommended)

### Build

```bash
# Clone the repository
git clone https://github.com/ca-srg/mdrag.git
cd mdRAG

# Install dependencies
go mod download

# Build
go build -o mdRAG

# Add executable to PATH (optional)
mv mdRAG /usr/local/bin/
```

## Commands

### 1. vectorize - Vectorization and S3 Storage

Read markdown files, extract metadata, generate embeddings using Amazon Bedrock, and store them in Amazon S3 Vectors.

```bash
mdRAG vectorize
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
mdRAG query -q "machine learning algorithms"

# Search with detailed options
mdRAG query --query "API documentation" --top-k 5 --json

# Search with metadata filter
mdRAG query -q "error handling" --filter '{"category":"programming"}'
```

**Options:**
- `-q, --query`: Search query text (required)
- `-k, --top-k`: Number of similar results to return (default: 10)
- `-j, --json`: Output results in JSON format
- `-f, --filter`: JSON metadata filter (e.g., `'{"category":"docs"}'`)

**Usage Examples:**
```bash
# Search technical documentation
mdRAG query -q "Docker container configuration" --top-k 3

# Search within specific category
mdRAG query -q "authentication" --filter '{"type":"security"}' --json

# Get more results
mdRAG query -q "database optimization" --top-k 20
```

### 3. list - List Vectors

Display a list of vectors stored in S3 Vector Index.

```bash
# Display all vectors
mdRAG list

# Filter by prefix
mdRAG list --prefix "docs/"
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
mdRAG chat

# Chat with custom context size
mdRAG chat --context-size 10

# Chat with custom weight balance for hybrid search
mdRAG chat --bm25-weight 0.7 --vector-weight 0.3

# Chat with custom system prompt
mdRAG chat --system "You are a helpful assistant specialized in documentation."
```

**Options:**
- `-c, --context-size`: Number of context documents to retrieve (default: 5)
- `-i, --interactive`: Run in interactive mode (default: true)
- `-s, --system`: System prompt for the chat
- `-b, --bm25-weight`: Weight for BM25 scoring in hybrid search (0-1, default: 0.5)
- `-v, --vector-weight`: Weight for vector scoring in hybrid search (0-1, default: 0.5)
- `--use-japanese-nlp`: Use Japanese NLP optimization for OpenSearch (default: true)

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

### 5. slack-bot - Slack Bot for RAG Search (New)

Start a Slack Bot that listens for mentions and answers with RAG results.

```bash
mdRAG slack-bot
```

Requirements:
- Set `SLACK_BOT_TOKEN` in your environment (see `.env.example`).
- Invite the bot user to the target Slack channel.
- Optionally enable threading with `SLACK_ENABLE_THREADING=true`.
 - Requires OpenSearch configuration (`OPENSEARCH_ENDPOINT`, `OPENSEARCH_INDEX`, `OPENSEARCH_REGION`). Slack Bot does not use S3 Vector fallback.

Details: see `docs/slack-bot.md`.

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
mdRAG/
├── main.go                 # Entry point
├── cmd/                    # CLI command definitions
│   ├── root.go            # Root command and common settings
│   ├── query.go           # query command
│   ├── list.go            # list command
│   ├── chat.go            # chat command
│   ├── slack.go           # slack-bot command (new)
│   └── vectorize.go       # vectorize command
├── internal/              # Internal libraries
│   ├── config/           # Configuration management
│   ├── embedding/        # Embedding generation
│   ├── s3vector/         # S3 Vector integration
│   ├── opensearch/       # OpenSearch integration
│   └── vectorizer/       # Vectorization service
├── markdown/             # Markdown documents (prepare before use)
├── export/               # Separate export tool for Kibela
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
   go build -o mdRAG-export
   ./mdRAG-export
   cd ..
   ```

3. **Vectorization and S3 Storage**
   ```bash
   # Verify with dry run
   mdRAG vectorize --dry-run
   
   # Execute actual vectorization
   mdRAG vectorize
   ```

4. **Check Vector Data**
   ```bash
   mdRAG list
   ```

5. **Execute Semantic Search**
   ```bash
   mdRAG query -q "content to search"
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

### Debugging Methods

```bash
# Execute with detailed logs
mdRAG vectorize --dry-run

# Check environment variables
env | grep AWS
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
  "https://your-opensearch-endpoint/_plugins/_security/api/roles/mdRAG_role" \
  -H "Content-Type: application/json" \
  -d '{
    "cluster_permissions": [
      "cluster:monitor/health",
      "indices:data/read/search"
    ],
    "index_permissions": [{
      "index_patterns": ["mdRAG-*"],
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
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/mdRAG_role" \
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

## Contributing

We welcome contributions to the project. Feel free to submit issues and pull requests.
