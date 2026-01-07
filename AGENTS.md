# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RAGent は Markdownドキュメントからハイブリッド検索（BM25 + ベクトル検索）を利用したRAGシステムを構築するツールです。Amazon S3 VectorsとOpenSearchを活用し、ベクトル化、セマンティック検索、対話型チャットの機能を提供します。

## Architecture

### Core Structure
- **main.go**: エントリーポイント、cobra CLIのExecuteを呼び出し
- **cmd/**: CLIコマンド定義
  - `root.go`: ルートコマンドと共通設定
  - `vectorize.go`: vectorizeコマンドの実装（ベクトル化とS3保存）
  - `query.go`: queryコマンドの実装（セマンティック検索）
  - `list.go`: listコマンドの実装（ベクトル一覧表示）
  - `chat.go`: chatコマンドの実装（対話的RAGクエリ）
  - `slack.go`: slackコマンドの実装（Slack Bot起動）
  - `mcp-server.go`: mcp-serverコマンドの実装（MCP Server起動） [NEW]

### Internal Packages
- **internal/vectorizer/**: ベクトル化サービス
  - VectorizerService: 並行処理によるベクトル化
  - ProcessingStats: 処理統計管理
  - エラーハンドリングとドライラン機能
- **internal/embedding/**: 埋め込み生成
  - `bedrock/`: Amazon Bedrock統合
  - 複数プロバイダー対応アーキテクチャ
- **internal/s3vector/**: S3 Vector統合
  - ベクトルストレージとインデックス管理
  - メタデータ付きベクトル保存
  - セマンティック検索機能
- **internal/config/**: 設定管理
  - 環境変数からの設定読み込み
  - 設定検証とデフォルト値
- **internal/scanner/**: ファイルスキャナー
  - ソースファイル（markdown/CSV）の再帰的発見
  - ファイルフィルタリング機能
- **internal/metadata/**: メタデータ抽出
  - FrontMatter解析
  - ファイル情報抽出
- **internal/csv/**: CSVファイル処理
  - `config.go`: CSV設定構造体（YAMLベース）
  - `reader.go`: CSVファイル読み込み・行展開
  - `column_detector.go`: 自動カラム検出
- **internal/filter/**: フィルタ機能
  - RAG検索時の除外フィルタロジック
  - S3 Vector対応フィルタ構築
  - ユーザーフィルタとの統合機能
- **internal/types/**: 共通型定義
  - システム全体で使用される構造体
  - `Config` に Slack検索 (`SLACK_SEARCH_*`) フィールドとデフォルト値を保持
- **internal/opensearch/**: OpenSearch統合（BM25 + Dense Hybrid RAG）
  - ハイブリッド検索エンジンの実装
  - BM25とベクトル検索の組み合わせ
  - 日本語最適化処理
  - エラーハンドリングと設定管理
- **internal/slacksearch/**: Slack検索パイプライン
  - クエリ生成、再検索、コンテキスト収集、十分性判定の各ステージをモジュール化
  - Slack Web APIとBedrock埋め込みクライアントを組み合わせたライブ検索
  - 進捗ハンドラーとOpenTelemetryトレーサーで観測可能性を担保
  - メッセージ、スレッド、タイムラインを`SlackSearchResult`として集約
- **internal/slackbot/**: Slack Bot統合
  - RTM WebSocket接続管理
  - メンション検出とメッセージ処理
  - RAG検索統合とレスポンス生成
  - Slack Block Kit形式でのフォーマット
- **internal/mcpserver/**: MCP Server統合 [NEW]
  - MCP SDK v0.4.0ベースの実装
  - JSON-RPC 2.0準拠プロトコル
  - OIDC認証とIPベース認証
  - デュアルトランスポート（HTTP/SSE）
  - ハイブリッド検索ツール提供

### Directories
- **source/**: RAGシステムで使用するソースドキュメント（MarkdownおよびCSV）を配置（使用前に準備が必要）
- **export/**: Kibelaノートエクスポート用の別ツール（独立したツール）
- **doc/**: プロジェクト文書（S3 Vector設定推奨など）
- **reference/**: 参考実装とサンプルコード

## Required Environment Variables

環境変数は `.env` ファイルで設定され、direnv (`.envrc`) により自動ロードされます:

### AWS設定
- `AWS_REGION`: AWSリージョン
- `AWS_ACCESS_KEY_ID`: AWS アクセスキーID
- `AWS_SECRET_ACCESS_KEY`: AWS シークレットアクセスキー

### S3 Vector設定
- `S3_VECTOR_INDEX_NAME`: S3 Vector インデックス名
- `S3_BUCKET_NAME`: S3バケット名
- `S3_VECTOR_REGION`: S3 Vectorバケット用AWSリージョン（デフォルト: us-east-1）
- `S3_SOURCE_REGION`: ソースファイル用S3バケットのAWSリージョン（デフォルト: us-east-1）

### OpenSearch設定（Hybrid RAG用）
- `OPENSEARCH_ENDPOINT`: OpenSearchエンドポイントURL
- `OPENSEARCH_USERNAME`: 認証用ユーザー名（オプション）
- `OPENSEARCH_PASSWORD`: 認証用パスワード（オプション）

### フィルタ設定
- `EXCLUDE_CATEGORIES`: RAG検索で除外するカテゴリ（カンマ区切り、デフォルト: "個人メモ,日報"）

### Slack Bot設定
- `SLACK_BOT_TOKEN`: Bot User OAuth Token (xoxb-...)
- `SLACK_RESPONSE_TIMEOUT`: レスポンスタイムアウト（デフォルト: 5s）
- `SLACK_MAX_RESULTS`: 最大検索結果数（デフォルト: 5）
- `SLACK_ENABLE_THREADING`: スレッド機能の有効化（デフォルト: false）

### Slack検索設定
- `SLACK_SEARCH_ENABLED`: Slack検索パイプラインの有効化（デフォルト: false）
- `SLACK_SEARCH_MAX_RESULTS`: Slack検索で取得するメッセージ件数（デフォルト: 20）
- `SLACK_SEARCH_MAX_RETRIES`: Slack APIの再試行回数（デフォルト: 5）
- `SLACK_SEARCH_CONTEXT_WINDOW_MINUTES`: タイムラインコンテキストを取得する時間幅（デフォルト: 30分）
- `SLACK_SEARCH_MAX_ITERATIONS`: 再探索イテレーションの最大数（デフォルト: 5）
- `SLACK_SEARCH_MAX_CONTEXT_MESSAGES`: Slackコンテキストとして蓄積する最大メッセージ数（デフォルト: 100）
- `SLACK_SEARCH_TIMEOUT_SECONDS`: Slack API タイムアウト秒数（デフォルト: 5秒）

### MCP Server設定 [NEW]
- `MCP_SERVER_HOST`: サーバーホスト（デフォルト: localhost）
- `MCP_SERVER_PORT`: サーバーポート（デフォルト: 8080）
- `MCP_IP_AUTH_ENABLED`: IP認証の有効化（デフォルト: true）
- `MCP_ALLOWED_IPS`: 許可IPアドレス（カンマ区切り）
- `OIDC_ISSUER`: OIDC プロバイダーURL
- `OIDC_CLIENT_ID`: OAuth2 クライアントID
- `OIDC_CLIENT_SECRET`: OAuth2 クライアントシークレット
- `OIDC_AUTH_URL`: カスタム認証エンドポイント（オプション）
- `OIDC_TOKEN_URL`: カスタムトークンエンドポイント（オプション）
- `OIDC_USERINFO_URL`: カスタムユーザー情報エンドポイント（オプション）
- `OIDC_JWKS_URL`: カスタムJWKSエンドポイント（オプション）

## Development Commands

```bash
# 依存関係の管理
go mod tidy
go mod download

# ビルド
go build -o RAGent

# テスト実行（テストファイルが存在する場合）
go test ./...

# フォーマット
go fmt ./...

# Lint（標準ツールを使用）
go vet ./...

# 各コマンドの実行例
go run main.go vectorize --dry-run       # ベクトル化（ドライラン）
go run main.go vectorize                 # ベクトル化実行
go run main.go vectorize --csv-config csv-config.yaml  # CSV設定を指定してベクトル化
go run main.go vectorize --follow        # フォローモード（30分間隔）
go run main.go vectorize --follow --interval 15m # カスタム間隔のフォローモード
go run main.go query -q "検索クエリ"      # セマンティック検索
go run main.go query -q "障害報告" --enable-slack-search --slack-channels "prod-incident" # Slack会話と併用
go run main.go chat                      # 対話的RAGチャット
SLACK_SEARCH_ENABLED=true go run main.go chat  # Slackコンテキスト付きチャット
go run main.go list                      # ベクトル一覧表示
go run main.go slack-bot                 # Slack Bot起動
go run main.go mcp-server                # MCP Server起動 [NEW]

# ベンダリング（禁止されている）
# go mod vendor は使用しない

# ドキュメント更新時の原則
# README.md と README_ja.md の両方を必ず更新する
```

## Prerequisites

ソースドキュメント（MarkdownまたはCSV）を`source/`ディレクトリに準備する必要があります。

**対応ファイル形式:**
- Markdown (.md, .markdown): 各ファイルが1つのドキュメントになります
- CSV (.csv): 各行が1つのドキュメントになります（ヘッダー行が必須）

CSVファイルのカラムマッピングは `--csv-config` オプションで設定できます。
Kibelaからのエクスポートには`export/`ディレクトリの別ツールを使用してください。

## Usage Examples

```bash
# 1. ベクトル化とS3保存
./RAGent vectorize --directory ./source --concurrency 10

# 1a. CSV設定を指定してベクトル化
./RAGent vectorize --csv-config csv-config.yaml

# 1b. フォローモードで継続的にベクトル化（30分間隔）
./RAGent vectorize --follow

# 1c. フォローモードで15分間隔に設定
./RAGent vectorize --follow --interval 15m
# ※ `--follow` は `--dry-run` および `--clear` と併用不可

# 2. セマンティック検索（ハイブリッドモード）
./RAGent query -q "機械学習のアルゴリズム" --top-k 5 --search-mode hybrid

# 2b. Slack会話も含めて検索
./RAGent query -q "インシデントタイムライン" --enable-slack-search --slack-channels "prod-incident,devops"

# 2a. OpenSearchのみ使用
./RAGent query -q "API documentation" --search-mode opensearch --bm25-weight 0.7

# 3. 対話的RAGチャット
./RAGent chat

# Slack検索を有効化したチャット
SLACK_SEARCH_ENABLED=true ./RAGent chat

# 4. ベクトル一覧表示
./RAGent list --prefix "docs/"

# 5. Slack Bot起動
./RAGent slack-bot
# Slackでの使用: @ragent-bot <質問内容>

# Slack検索を有効化したBot（環境変数）
SLACK_SEARCH_ENABLED=true SLACK_SEARCH_MAX_RESULTS=30 ./RAGent slack-bot

# 6. MCP Server起動 [NEW]
./RAGent mcp-server --auth-method either
# Claude Desktopでの使用: MCP統合によるハイブリッド検索
```

Slack結果を返すMCPツールリクエスト例:

```json
{
  "jsonrpc": "2.0",
  "id": "example",
  "method": "tools/call",
  "params": {
    "name": "ragent-hybrid_search",
    "arguments": {
      "query": "リリースメモ",
      "enable_slack_search": true,
      "slack_channels": ["release-notes"]
    }
  }
}
```

## Dependencies

### Core Framework
- `github.com/spf13/cobra`: CLIフレームワーク
- `github.com/joho/godotenv`: 環境変数読み込み
- `gopkg.in/yaml.v3`: YAML設定ファイル処理

### AWS Integration
- `github.com/aws/aws-sdk-go-v2`: AWS SDK v2
- `github.com/aws/aws-sdk-go-v2/config`: AWS設定管理
- `github.com/aws/aws-sdk-go-v2/service/s3`: S3操作
- `github.com/aws/aws-sdk-go-v2/service/s3vectors`: S3 Vector操作
- `github.com/aws/aws-sdk-go-v2/service/bedrockruntime`: Bedrock Runtime操作

### Search & Processing
- `github.com/opensearch-project/opensearch-go/v4`: OpenSearch クライアント
- `github.com/netflix/go-env`: 環境変数処理
- `golang.org/x/sync`: 並行処理制御
- `golang.org/x/time`: レート制限

### Slack Integration
- `github.com/slack-go/slack`: Slack API クライアント（RTM API対応）

### MCP Integration [NEW]
- `github.com/modelcontextprotocol/go-sdk`: MCP公式SDK v0.4.0
- `github.com/coreos/go-oidc`: OpenID Connect実装
- JSON-RPC 2.0プロトコル対応

## Configuration

設定は環境変数から読み込まれます。

### Vector Search設定
**S3 Vector推奨設定:**
- ディメンション: 1024次元（Amazon Titan Text Embedding v2）
- 距離メトリック: コサイン距離
- 対象: 6,583個の日本語ビジネス文書

**OpenSearch設定（Hybrid RAG用）:**
- Dense検索: 100件
- BM25検索: 200件
- 日本語最適化: kuromoji tokenizer
- ハイブリッドスコア融合アルゴリズム対応

## Active Specifications

### slack-bot-app
Slack Bot機能を追加し、SlackでBotにメンションすることでRAG検索を実行できるようにする。RTM APIを利用し、RAGentのchatコマンドと同等の機能を提供。

**Status**: completed
**Branch**: `main`

### mcp-server-integration [NEW]
MCP (Model Context Protocol) Server機能を追加し、Claude DesktopやMCP対応ツールからRAGentのハイブリッド検索機能を利用できるようにする。OIDC認証、IPベース認証、複数認証方式に対応。

**Status**: completed
**Branch**: `mcp`

### opensearch-bm25-dense-rag
Dense@100 + BM25@200（日本語最適化）を利用してRAGの精度を向上させる。AWS の OpenSearch を利用し、OpenSearch（BM25＋k-NN）で実現する。

**Status**: initialized
**Next Step**: `/kiro:spec-requirements opensearch-bm25-dense-rag`

### vectorize-opensearch-indexing
既存のvectorizeコマンドにOpenSearch機能を統合し、BM25+k-NNを利用したインデックス作成機能を追加する。

**Status**: initialized
**Next Step**: `/kiro:spec-requirements vectorize-opensearch-indexing`
