# kiberag - Kibela API Gateway

**[日本語版 (Japanese) / 日本語 README](README_ja.md)**

kiberag is a CLI tool that retrieves all notes from the Kibela GraphQL API, exports them as markdown files with appropriate metadata, and serves as a RAG (Retrieval-Augmented Generation) system through vector data storage in Amazon S3 Vectors.

## Features

- **Note Export**: Retrieve all notes from Kibela GraphQL API and save them as markdown files
- **Vectorization**: Convert markdown files to embeddings using Voyage-3-large model
- **S3 Vector Integration**: Store generated vectors in Amazon S3 Vectors
- **Semantic Search**: Semantic similarity search using S3 Vector Index
- **Vector Management**: List vectors stored in S3

## Required Environment Variables

Create a `.env` file in the project root and configure the following environment variables:

```env
# Kibela API Configuration
KIBELA_TOKEN=your_kibela_api_token
KIBELA_TEAM=your_team_name

# AWS Configuration
AWS_REGION=your_aws_region
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# S3 Vector Configuration
S3_VECTOR_INDEX_NAME=your_vector_index_name
S3_BUCKET_NAME=your_s3_bucket_name

# Voyage AI Configuration
VOYAGE_API_KEY=your_voyage_api_key
```

## Installation

### Prerequisites

- Go 1.25.0 or higher
- direnv (recommended)

### Build

```bash
# Clone the repository
git clone https://github.com/rluisr/kiberag.git
cd kiberag

# Install dependencies
go mod download

# Build
go build -o kiberag

# Add executable to PATH (optional)
mv kiberag /usr/local/bin/
```

## Commands

### 1. export - Export Notes

Retrieve all notes from Kibela GraphQL API and save them as markdown files in the `markdown/` directory.

```bash
kiberag export
```

**Features:**
- Fetch all notes from Kibela API
- Add appropriate metadata
- Automatic filename generation
- Automatic category extraction
- Save to `markdown/` directory

### 2. vectorize - Vectorization and S3 Storage

Read markdown files, extract metadata, generate embeddings using Voyage AI, and store them in Amazon S3 Vectors.

```bash
kiberag vectorize
```

**Options:**
- `-d, --directory`: Directory containing markdown files to process (default: `./markdown`)
- `--dry-run`: Display processing details without making actual API calls
- `-c, --concurrency`: Number of concurrent processes (0 = use default value from config file)

**Features:**
- Recursive scanning of markdown files
- Automatic metadata extraction
- Embedding generation using Voyage-3-large model
- Safe storage to S3 Vectors
- High-speed processing through concurrency

### 3. query - Semantic Search

Execute semantic similarity search against S3 Vector Index.

```bash
# Basic search
kiberag query -q "machine learning algorithms"

# Search with detailed options
kiberag query --query "API documentation" --top-k 5 --json

# Search with metadata filter
kiberag query -q "error handling" --filter '{"category":"programming"}'
```

**Options:**
- `-q, --query`: Search query text (required)
- `-k, --top-k`: Number of similar results to return (default: 10)
- `-j, --json`: Output results in JSON format
- `-f, --filter`: JSON metadata filter (e.g., `'{"category":"docs"}'`)

**Usage Examples:**
```bash
# Search technical documentation
kiberag query -q "Docker container configuration" --top-k 3

# Search within specific category
kiberag query -q "authentication" --filter '{"type":"security"}' --json

# Get more results
kiberag query -q "database optimization" --top-k 20
```

### 4. list - List Vectors

Display a list of vectors stored in S3 Vector Index.

```bash
# Display all vectors
kiberag list

# Filter by prefix
kiberag list --prefix "docs/"
```

**Options:**
- `-p, --prefix`: Prefix to filter vector keys

**Features:**
- Display stored vector keys
- Filtering by prefix
- Check vector database contents

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
kiberag/
├── main.go                 # Entry point
├── cmd/                    # CLI command definitions
│   ├── root.go            # Root command and common settings
│   ├── export.go          # export command
│   ├── query.go           # query command
│   ├── list.go            # list command
│   └── vectorize.go       # vectorize command
├── internal/              # Internal libraries
│   ├── kibera/           # Kibela GraphQL API client
│   └── export/           # Export functionality
├── markdown/             # Exported markdown files
├── .envrc                # direnv configuration
├── .env                  # Environment variables file
└── CLAUDE.md            # Claude Code configuration
```

## Dependencies

### Core Libraries

- **github.com/spf13/cobra**: CLI framework
- **github.com/machinebox/graphql**: GraphQL client
- **github.com/joho/godotenv**: Environment variable loader
- **github.com/aws/aws-sdk-go-v2**: AWS SDK v2
  - S3 service
  - S3 Vectors
  - Bedrock Runtime (Voyage AI integration)
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

2. **Export Notes**
   ```bash
   kiberag export
   ```

3. **Vectorization and S3 Storage**
   ```bash
   # Verify with dry run
   kiberag vectorize --dry-run
   
   # Execute actual vectorization
   kiberag vectorize
   ```

4. **Check Vector Data**
   ```bash
   kiberag list
   ```

5. **Execute Semantic Search**
   ```bash
   kiberag query -q "content to search"
   ```

## Troubleshooting

### Common Errors

1. **Environment variables not set**
   ```
   Error: required environment variable not set
   ```
   → Check if `.env` file is properly configured

2. **Kibela API connection error**
   ```
   Error: failed to connect to Kibela API
   ```
   → Verify `KIBELA_TOKEN` and `KIBELA_TEAM` are correct

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
kiberag vectorize --dry-run

# Check environment variables
env | grep KIBELA
env | grep AWS
env | grep VOYAGE
```

## License

For license information, please refer to the LICENSE file in the repository.

## Contributing

We welcome contributions to the project. Feel free to submit issues and pull requests.