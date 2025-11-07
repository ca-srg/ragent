# RAGent - Markdownドキュメント用RAGシステム構築ツール

**[English README](README.md)**

RAGent は、Markdownドキュメントからハイブリッド検索（BM25 + ベクトル検索）を利用したRAG（Retrieval-Augmented Generation）システムを構築するCLIツールです。Amazon S3 VectorsとOpenSearchを活用した高精度な検索機能を提供します。

## 目次

- [機能](#機能)
- [Slack検索統合](#slack検索統合)
- [Embedding非依存RAG](#embedding非依存rag)
- [アーキテクチャ概要](#アーキテクチャ概要)
- [前提条件](#前提条件)
- [必要な環境変数](#必要な環境変数)
- [インストール](#インストール)
- [コマンド一覧](#コマンド一覧)
  - [vectorize - ベクトル化とS3保存](#1-vectorize---ベクトル化とs3保存)
  - [query - セマンティック検索](#2-query---セマンティック検索)
  - [list - ベクトル一覧表示](#3-list---ベクトル一覧表示)
  - [chat - 対話型RAGチャット](#4-chat---対話型ragチャット)
  - [slack-bot - Slack Bot（メンションでRAG検索）](#5-slack-bot---slack-botメンションでrag検索)
  - [mcp-server - Claude Desktop統合用MCPサーバー（新機能）](#6-mcp-server---claude-desktop統合用mcpサーバー新機能)
- [開発](#開発)
- [典型的なワークフロー](#典型的なワークフロー)
- [トラブルシューティング](#トラブルシューティング)
- [OpenSearch RAG設定](#opensearch-rag設定)
- [セットアップ自動化（setup.sh）](#セットアップ自動化setupsh)
- [MCPサーバー統合](#mcpサーバー統合)
- [ライセンス](#ライセンス)
- [貢献](#貢献)

## 機能

- **ベクトル化**: markdownファイルをAmazon Bedrockを使用してembeddingに変換
- **S3 Vector統合**: 生成されたベクトルをAmazon S3 Vectorsに保存
- **ハイブリッド検索**: OpenSearchを使用したBM25 + ベクトル検索の組み合わせ
- **Slack検索統合**: Slack会話とドキュメント検索結果を統合する反復型パイプライン
- **セマンティック検索**: S3 Vector Indexを使用したセマンティック類似性検索
- **対話型RAGチャット**: コンテキスト認識応答を行うチャットインターフェース
- **ベクトル管理**: S3に保存されたベクトルの一覧表示
- **Embedding非依存RAG**: 事前ベクトル化なしでSlackから直接回答を生成する検索経路
- **IPベースセキュリティ**: 許可IP制御に加え、バイパスレンジと監査ログを設定可能

## Slack検索統合

`SLACK_SEARCH_ENABLED=true` を設定すると、以下のワークフローでSlack検索が自動的に動作します。

- `query` コマンドは `--enable-slack-search` と `--slack-channels` フラグでリクエスト単位の制御が可能。
- `chat` コマンドは追加フラグなしで最新のSlack会話を取り込み、進捗を表示します。
- `slack-bot` は回答内に「Conversations from Slack」セクションを追加し、各メッセージへのパーマリンクを提供します。
- `mcp-server` の `hybrid_search` ツールは `enable_slack_search` と `slack_channels` パラメータを受け付けます。

各検索ではクエリの言い換え、タイムライン展開、十分性判定を行い、ドキュメント結果と一体化したレスポンスを生成します。

```bash
# ドキュメントとSlackを同時に検索
RAGent query -q "インシデントタイムライン" --enable-slack-search --slack-channels "prod-incident,devops"
```

## Embedding非依存RAG

Slack検索はベクトルの事前生成に依存せず、クエリ時に最新メッセージを取得する設計です。これにより以下のメリットがあります。

- **運用コストの削減**: Slackデータ用の追加ベクトルストアや夜間バッチは不要。
- **リアルタイム性**: 投稿直後のメッセージも回答に反映できるため、障害対応やリリース情報が遅延しません。
- **時系列の保持**: スレッド構造と投稿順を維持したまま文脈を提示できます。
- **シームレスなフォールバック**: Slackでヒットしない場合でも従来のドキュメント検索が継続します。

## アーキテクチャ概要

下図はドキュメント検索とSlack検索がどのように合流するかを示しています。

```mermaid
graph LR
    MD[Markdown Documents] -->|Vectorize| VE[Amazon S3 Vectors]
    VE --> HY[Hybrid Search Engine]
    OS[(Amazon OpenSearch)] --> HY
    SL[Slack Workspace] -->|Conversations API| SS[SlackSearch Service]
    SS --> HY
    HY --> CT[Context Builder]
    CT --> AN[Answer Generation (Claude, Bedrock Chat)]
```

### 詳細アーキテクチャ図

#### コマンド概要

RAGentは5つの主要コマンドを提供し、それぞれRAGワークフローで特定の役割を果たします：

```mermaid
flowchart LR
    User([ユーザー])

    User -->|1| Vectorize[vectorize<br/>Markdown → ベクトル]
    User -->|2| Query[query<br/>セマンティック検索]
    User -->|3| Chat[chat<br/>対話型RAG]
    User -->|4| SlackBot[slack-bot<br/>Slack統合]
    User -->|5| MCPServer[mcp-server<br/>Claude Desktop]

    Vectorize -->|保存| S3V[(S3 Vectors)]
    Vectorize -->|インデックス| OS[(OpenSearch)]

    Query --> Hybrid[ハイブリッド検索<br/>BM25 + ベクトル]
    Chat --> Hybrid
    SlackBot --> Hybrid
    MCPServer --> Hybrid

    Hybrid --> S3V
    Hybrid --> OS
    Hybrid -->|オプション| Slack[(Slack API)]

    Hybrid --> Results[検索結果]
    Results --> Answer[生成された回答<br/>via Bedrock Claude]

    style Vectorize fill:#e1f5ff
    style Query fill:#fff4e1
    style Chat fill:#ffe1f5
    style SlackBot fill:#e1ffe1
    style MCPServer fill:#f5e1ff
```

#### ハイブリッド検索フロー

ハイブリッド検索エンジンは、BM25キーワードマッチングとベクトル類似性検索を組み合わせます：

```mermaid
sequenceDiagram
    participant User as ユーザー
    participant CLI as CLIコマンド
    participant Bedrock as Amazon Bedrock
    participant OS as OpenSearch
    participant Slack as Slack API
    participant Engine as ハイブリッドエンジン

    User->>CLI: query "検索クエリ"
    CLI->>Bedrock: GenerateEmbedding(query)
    Bedrock-->>CLI: Vector[1024]

    par BM25検索
        CLI->>OS: BM25検索 (kuromoji tokenizer)
        OS-->>CLI: BM25結果 (200件)
    and ベクトル検索
        CLI->>OS: k-NN検索 (コサイン類似度)
        OS-->>CLI: ベクトル結果 (100件)
    end

    CLI->>Engine: 結果融合 (RRF/weighted_sum)
    Engine-->>CLI: 融合結果 (top-k)

    opt Slack検索有効時
        CLI->>Slack: search.messages(query)
        Slack-->>CLI: Slackメッセージ
        CLI->>Slack: conversations.history(threads)
        Slack-->>CLI: スレッドコンテキスト
        CLI->>Engine: Slackコンテキストをマージ
        Engine-->>CLI: 拡張結果
    end

    CLI-->>User: 統合結果 + 参考文献
```

#### Slack Bot処理フロー

Slack Botはメンションをリッスンし、RAG駆動の回答を返します：

```mermaid
sequenceDiagram
    participant Slack as Slackワークスペース
    participant Bot as Slack Bot (RTM/Socket)
    participant Detector as メンション検出
    participant Extractor as クエリ抽出
    participant Hybrid as ハイブリッド検索
    participant SlackSearch as Slack検索サービス
    participant Formatter as Block Kitフォーマッター

    Slack->>Bot: @ragent-bot "質問内容"
    Bot->>Detector: メンション検出
    Detector-->>Bot: メンションイベント

    Bot->>Extractor: クエリ抽出
    Extractor-->>Bot: クエリテキスト

    Bot->>Hybrid: Search(query)

    par ドキュメント検索
        Hybrid->>Hybrid: BM25 + k-NN
        Hybrid-->>Bot: ドキュメント結果
    and Slackコンテキスト検索（オプション）
        Bot->>SlackSearch: SearchSlack(query, channels)
        SlackSearch->>SlackSearch: クエリ改良ループ
        SlackSearch->>Slack: search.messages API
        Slack-->>SlackSearch: メッセージ
        SlackSearch->>Slack: conversations.history (threads)
        Slack-->>SlackSearch: スレッドタイムライン
        SlackSearch->>SlackSearch: 十分性チェック
        SlackSearch-->>Bot: Slackコンテキスト
    end

    Bot->>Formatter: 結果フォーマット (Block Kit)
    Formatter-->>Bot: Slackブロック

    Bot->>Slack: メッセージ投稿 (ブロック付き)
    Slack-->>Slack: 回答 + 参考文献を表示
```

#### MCP Server統合

MCP ServerはRAGentのハイブリッド検索をClaude DesktopなどのMCP対応ツールに公開します：

```mermaid
sequenceDiagram
    participant Claude as Claude Desktop
    participant MCP as MCPサーバー
    participant Auth as 認証ミドルウェア
    participant Handler as ハイブリッド検索ハンドラ
    participant Bedrock as Amazon Bedrock
    participant OS as OpenSearch
    participant Slack as Slack API

    Claude->>MCP: JSON-RPCリクエスト<br/>tools/call "hybrid_search"

    MCP->>Auth: リクエスト認証

    alt IP認証
        Auth->>Auth: クライアントIPチェック
    else OIDC認証
        Auth->>Auth: JWTトークン検証
    else バイパスレンジ
        Auth->>Auth: バイパスCIDRチェック
    end

    Auth-->>MCP: 認証OK

    MCP->>Handler: hybrid_search(query, options)を処理

    Handler->>Bedrock: GenerateEmbedding(query)
    Bedrock-->>Handler: Vector[1024]

    par OpenSearch BM25
        Handler->>OS: BM25クエリ
        OS-->>Handler: BM25結果
    and OpenSearch k-NN
        Handler->>OS: k-NNクエリ
        OS-->>Handler: ベクトル結果
    end

    Handler->>Handler: 結果融合 (weighted_sum/RRF)

    opt enable_slack_search=true
        Handler->>Slack: メッセージ + スレッド検索
        Slack-->>Handler: Slackコンテキスト
        Handler->>Handler: Slack結果をマージ
    end

    Handler-->>MCP: 検索結果 (JSON)
    MCP-->>Claude: JSON-RPCレスポンス<br/>{results, slack_results, metadata}
```

#### ベクトル化パイプライン

vectorizeコマンドはMarkdownドキュメントを処理し、デュアルバックエンドに保存します：

```mermaid
flowchart TD
    Start([vectorize コマンド]) --> CheckFlags{フラグ確認}

    CheckFlags -->|--clear| Clear[全ベクトル削除<br/>+ インデックス再作成]
    CheckFlags -->|通常| Scan
    Clear --> Scan

    Scan[Markdownディレクトリスキャン] --> Files[.mdファイルリスト化]

    Files --> Loop{各ファイル処理<br/>並行実行}

    Loop --> Read[ファイル内容読み込み]
    Read --> Extract[メタデータ抽出<br/>FrontMatterパーサー]
    Extract --> Embed[埋め込み生成<br/>Bedrock Titan v2]

    Embed --> Dual{デュアルバックエンド保存}

    Dual --> S3[S3 Vectorへ保存<br/>メタデータ付き]
    Dual --> OSIndex[OpenSearchへインデックス<br/>BM25 + k-NNフィールド]

    S3 --> Stats1[統計更新<br/>S3: 成功/失敗]
    OSIndex --> Stats2[統計更新<br/>OS: インデックス済/スキップ/リトライ]

    Stats1 --> Check{さらにファイル?}
    Stats2 --> Check

    Check -->|はい| Loop
    Check -->|いいえ| Report[統計表示<br/>成功率、エラー詳細]

    Report --> Follow{フォローモード?}
    Follow -->|はい| Wait[インターバル待機<br/>デフォルト: 30分]
    Wait --> Scan
    Follow -->|いいえ| End([完了])

    style Start fill:#e1f5ff
    style Clear fill:#ffe1e1
    style Embed fill:#fff4e1
    style Dual fill:#f5e1ff
    style End fill:#e1ffe1
```

## 前提条件

### Markdownドキュメントの準備

RAGentを使用する前に、`markdown/` ディレクトリにMarkdownドキュメントを準備する必要があります。これらのドキュメントがRAGシステムで検索可能なコンテンツとなります。

```bash
# markdownディレクトリを作成
mkdir markdown

# markdownファイルをディレクトリに配置
cp /path/to/your/documents/*.md markdown/
```

Kibelaからノートをエクスポートする場合は、`export/` ディレクトリにある別ツールをご利用ください。

## 必要な環境変数

プロジェクトルートに `.env` ファイルを作成し、以下の環境変数を設定してください：

```env
# AWS設定
AWS_REGION=your_aws_region
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# S3 Vector設定
S3_VECTOR_INDEX_NAME=your_vector_index_name
S3_BUCKET_NAME=your_s3_bucket_name

# OpenSearch設定（ハイブリッドRAG用）
OPENSEARCH_ENDPOINT=your_opensearch_endpoint
OPENSEARCH_INDEX=your_opensearch_index
OPENSEARCH_REGION=us-east-1  # デフォルト

# チャット設定
CHAT_MODEL=anthropic.claude-3-5-sonnet-20240620-v1:0  # デフォルト
EXCLUDE_CATEGORIES=個人メモ,日報  # 検索から除外するカテゴリ

# Slack Bot設定
SLACK_BOT_TOKEN=xoxb-your-bot-token
SLACK_USER_TOKEN=xoxp-your-user-token-with-search-read
SLACK_RESPONSE_TIMEOUT=5s
SLACK_MAX_RESULTS=5
SLACK_ENABLE_THREADING=false
SLACK_THREAD_CONTEXT_ENABLED=true
SLACK_THREAD_CONTEXT_MAX_MESSAGES=10

# Slack検索設定
SLACK_SEARCH_ENABLED=false                     # Slack検索パイプラインを有効化
SLACK_SEARCH_MAX_RESULTS=20                    # 1クエリで取得するメッセージ数 (1-100)
SLACK_SEARCH_MAX_RETRIES=5                     # rate limit時の再試行回数 (0-10)
SLACK_SEARCH_CONTEXT_WINDOW_MINUTES=30         # 周辺メッセージを取得する時間範囲（分）
SLACK_SEARCH_MAX_ITERATIONS=5                  # 不足情報がある場合の再探索回数
SLACK_SEARCH_MAX_CONTEXT_MESSAGES=100          # コンテキストとして蓄積する最大メッセージ数
SLACK_SEARCH_TIMEOUT_SECONDS=5                 # Slack APIリクエストのタイムアウト秒数 (1-60)

# OpenTelemetry設定（任意）
OTEL_ENABLED=false
OTEL_SERVICE_NAME=ragent
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=service.namespace=ragent,environment=dev
OTEL_TRACES_SAMPLER=always_on
OTEL_TRACES_SAMPLER_ARG=1.0

# MCPバイパス設定（任意）
MCP_BYPASS_IP_RANGE=10.0.0.0/8,172.16.0.0/12  # カンマ区切りのCIDRレンジ
MCP_BYPASS_VERBOSE_LOG=false
MCP_BYPASS_AUDIT_LOG=true
MCP_TRUSTED_PROXIES=192.168.1.1,10.0.0.1  # X-Forwarded-Forを信頼するプロキシ
```

Slack検索を利用する場合は、`SLACK_SEARCH_ENABLED=true`・`SLACK_BOT_TOKEN` に加えて、`search:read` や `channels:history` / `groups:history` など必要なスコープを付与した `SLACK_USER_TOKEN` を同じワークスペースで設定してください。Slack検索用の環境変数でスループットや動作を調整できます。

### MCPバイパス設定（任意）

- `MCP_BYPASS_IP_RANGE`: 認証をスキップする信頼済みネットワークをCIDR形式でカンマ区切り指定します。
- `MCP_BYPASS_VERBOSE_LOG`: バイパス判定の詳細ログを有効化します。ロールアウト時の検証に便利です。
- `MCP_BYPASS_AUDIT_LOG`: バイパスされたリクエストをJSON監査ログとして出力します（デフォルト有効）。
- `MCP_TRUSTED_PROXIES`: バイパス判定時に `X-Forwarded-For` を信頼するプロキシIPをカンマ区切りで指定します。

## OpenTelemetryによる可観測性

RAGentはOpenTelemetry (OTel) Go SDK を利用してトレースとメトリクスを公開します。Slack Botのメッセージ処理、MCPツール呼び出し、ハイブリッド検索処理にスパンが付与され、リクエスト数・エラー数・レスポンス時間のメトリクスが収集されます。

### 有効化方法

以下の環境変数を設定します（`.env.example` 参照）：

- `OTEL_ENABLED`: `true` で有効化（デフォルト `false`）
- `OTEL_SERVICE_NAME`: サービス名（デフォルト `ragent`）
- `OTEL_RESOURCE_ATTRIBUTES`: `service.namespace=ragent,environment=dev` のような `key=value` 形式
- `OTEL_EXPORTER_OTLP_ENDPOINT`: OTLPエンドポイントURL（スキームを含む）
- `OTEL_EXPORTER_OTLP_PROTOCOL`: `http/protobuf`（デフォルト）または `grpc`
- `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`: サンプリング設定（`always_on`, `traceidratio` など）

※ `OTEL_ENABLED=false` の場合、Tracer/Meterはnoopとなりオーバーヘッドは発生しません。

### Jaeger 連携例（ローカル）

```bash
# 1. Jaeger (all-in-one) を起動
docker run --rm -it -p 4318:4318 -p 16686:16686 jaegertracing/all-in-one:1.58

# 2. RAGent を実行するターミナルで環境変数をセット
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf

# 3. Slack Bot などを実行
go run main.go slack-bot
# http://localhost:16686 でトレースを確認
```

### Prometheus + OpenTelemetry Collector 連携例

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
# http://localhost:9464/metrics をPrometheusでスクレイプ
```

### AWS X-Ray + ADOT Collector 連携例

```bash
docker run --rm -it -p 4317:4317 public.ecr.aws/aws-observability/aws-otel-collector:latest
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_TRACES_SAMPLER=traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.2
```

### 収集されるメトリクス／スパン

- **スパン**
  - `slackbot.process_message`
  - `mcpserver.hybrid_search`
  - `search.hybrid`
- **メトリクス**
  - `ragent.slack.requests.total`, `ragent.slack.errors.total`, `ragent.slack.response_time`
  - `ragent.mcp.requests.total`, `ragent.mcp.errors.total`, `ragent.mcp.response_time`

チャネル種別、認証方式、ツール名、検索結果件数などが属性として付与されるため、ダッシュボードで簡単にフィルタリングできます。

### Grafana ダッシュボード

RAGentには、OpenTelemetryメトリクスを可視化するための事前設定済みGrafanaダッシュボードテンプレートが含まれています。このダッシュボードは、Slack BotとMCPサーバーの包括的な監視を提供します。

![Grafana Dashboard](assets/grafana.png)

**ダッシュボードパネル:**
- **Slackメトリクス**
  - Slack Botリクエスト（時系列）
  - Slack応答レイテンシ（ヒストグラム）
  - Slackエラー率（ゲージ）
- **MCPメトリクス**
  - MCPリクエスト（時系列）
  - MCP応答時間（ヒストグラム）
  - MCPエラー率（ゲージ）

**セットアップ手順:**

1. RAGentでOpenTelemetryを有効化:
   ```bash
   export OTEL_ENABLED=true
   export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
   ```

2. OTel CollectorをPrometheusにメトリクスをエクスポートするよう設定（上記の例を参照）

3. Grafanaにダッシュボードテンプレートをインポート:
   ```bash
   # Grafana APIを使用
   curl -X POST http://localhost:3000/api/dashboards/import \
     -H "Content-Type: application/json" \
     -d @assets/grafana.json
   
   # または、Grafana UIから手動でインポート
   # Dashboard > Import > Upload JSON file > assets/grafana.json を選択
   ```

4. Grafanaでメトリクスエンドポイントを指すPrometheusデータソースを設定

RAGentがリクエストの処理を開始すると、ダッシュボードは自動的にメトリクスを表示し始めます。

## インストール

### 前提条件

- Go 1.25.0以上
- direnv（推奨）

### ビルド

```bash
# リポジトリをクローン
git clone https://github.com/ca-srg/ragent.git
cd RAGent

# 依存関係をインストール
go mod download

# ビルド
go build -o RAGent

# 実行可能ファイルをPATHに追加（オプション）
mv RAGent /usr/local/bin/
```

## コマンド一覧

### 1. vectorize - ベクトル化とS3保存

markdownファイルを読み込み、メタデータを抽出し、Amazon Bedrockを使用してembeddingを生成してAmazon S3 Vectorsに保存します。

```bash
RAGent vectorize
```

**オプション:**
- `-d, --directory`: 処理するmarkdownファイルのディレクトリ（デフォルト: `./markdown`）
- `--dry-run`: 実際のAPI呼び出しを行わずに処理内容を表示
- `-c, --concurrency`: 並行処理数（0 = 設定ファイルのデフォルト値を使用）

**機能:**
- markdownファイルの再帰的スキャン
- メタデータの自動抽出
- Amazon Titan Text Embedding v2モデルを使用したembedding生成
- S3 Vectorsへの安全な保存
- 並行処理による高速化

### 2. query - セマンティック検索

S3 Vector Indexに対してセマンティック類似性検索を実行します。

```bash
# 基本的な検索
RAGent query -q "machine learning algorithms"

# 詳細オプション付きの検索
RAGent query --query "API documentation" --top-k 5 --json

# メタデータフィルター付きの検索
RAGent query -q "error handling" --filter '{"category":"programming"}'
```

**オプション:**
- `-q, --query`: 検索クエリテキスト（必須）
- `-k, --top-k`: 返される類似結果の数（デフォルト: 10）
- `-j, --json`: 結果をJSON形式で出力
- `-f, --filter`: JSONメタデータフィルター（例: `'{"category":"docs"}'`）
- `--enable-slack-search`: Slack検索を有効化し、ドキュメント結果と併せて表示
- `--slack-channels`: Slack検索対象のチャンネル名をカンマ区切りで指定（先頭の`#`は不要）

**使用例:**
```bash
# 技術文書の検索
RAGent query -q "Docker コンテナ設定" --top-k 3

# 特定カテゴリでの検索
RAGent query -q "authentication" --filter '{"type":"security"}' --json

# より多くの結果を取得
RAGent query -q "database optimization" --top-k 20

# Slackと併せてインシデントレビューを検索
RAGent query -q "オンコール引き継ぎ" --enable-slack-search --slack-channels "oncall,incident-review"
```

#### URL対応検索

RAGent はクエリ内の HTTP/HTTPS URL を検出し、まず `reference` フィールドに対する完全一致の term query を実行します。

- URL が検出され一致した場合は `search_method` が `"url_exact_match"` となり、URL に紐づく結果のみを即座に返します。
- term query が失敗またはヒットゼロの場合はハイブリッド検索 (`"hybrid_search"`) にフォールバックし、`fallback_reason` に理由 (`term_query_error` / `term_query_no_results`) を記録します。
- CLI の `--json` 出力、Slack Bot、MCP ツールのレスポンスには `search_method`, `url_detected`, `fallback_reason` が含まれ、検索経路を確認できます。

```bash
RAGent query --json -q "https://example.com/doc のタイトルを教えて"
```

JSON レスポンス例:

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

URL が見つからない場合や一致しなかった場合は `search_method` が `"hybrid_search"` になり、通常の BM25 + ベクトル融合結果が返されます。

### 3. list - ベクトル一覧表示

S3 Vector Indexに保存されているベクトルの一覧を表示します。

```bash
# 全ベクトルを表示
RAGent list

# プレフィックスでフィルタリング
RAGent list --prefix "docs/"
```

**オプション:**
- `-p, --prefix`: ベクトルキーをフィルタリングするプレフィックス

**機能:**
- 保存されたベクトルキーの表示
- プレフィックスによるフィルタリング
- ベクトルデータベースの内容確認

### 4. chat - 対話型RAGチャット

ハイブリッド検索（OpenSearch BM25 + ベクトル検索）を使用してコンテキストを取得し、Amazon Bedrock（Claude）で応答を生成する対話型チャットセッションを開始します。

```bash
# デフォルト設定で対話型チャットを開始
RAGent chat

# カスタムコンテキストサイズでチャット
RAGent chat --context-size 10

# ハイブリッド検索の重みバランスをカスタマイズ
RAGent chat --bm25-weight 0.7 --vector-weight 0.3

# カスタムシステムプロンプトでチャット
RAGent chat --system "あなたはドキュメントに特化した親切なアシスタントです。"

# Slackコンテキストを取り込んでチャット（要: SLACK_SEARCH_ENABLED=true）
SLACK_SEARCH_ENABLED=true RAGent chat
```

**オプション:**
- `-c, --context-size`: 取得するコンテキストドキュメント数（デフォルト: 5）
- `-i, --interactive`: 対話モードで実行（デフォルト: true）
- `-s, --system`: チャット用のシステムプロンプト
- `-b, --bm25-weight`: ハイブリッド検索でのBM25スコアリングの重み（0-1、デフォルト: 0.5）
- `-v, --vector-weight`: ハイブリッド検索でのベクトルスコアリングの重み（0-1、デフォルト: 0.5）
- `--use-japanese-nlp`: OpenSearchで日本語NLP最適化を使用（デフォルト: true）

`SLACK_SEARCH_ENABLED=true` を設定した場合、チャットはSlackの会話を自動取得し、各イテレーションの進捗とパーマリンク付き結果を表示します。

**機能:**
- BM25とベクトル類似性を組み合わせたハイブリッド検索
- 取得したドキュメントを使用したコンテキスト認識応答
- 会話履歴管理
- ソースリンク付き参考文献引用
- 日本語最適化

**チャットコマンド:**
- `exit` または `quit`: チャットセッションを終了
- `clear`: 会話履歴をクリア
- `help`: 利用可能なコマンドを表示

## 開発

### ビルドコマンド

```bash
# フォーマット
go fmt ./...

# 依存関係の整理
go mod tidy

# テスト実行（設定されている場合）
go test ./...

# 開発用実行
go run main.go [command]
```

### プロジェクト構造

```
RAGent/
├── main.go                 # エントリーポイント
├── cmd/                    # CLIコマンド定義
│   ├── root.go            # ルートコマンドと共通設定
│   ├── query.go           # queryコマンド
│   ├── list.go            # listコマンド
│   ├── chat.go            # chatコマンド
│   ├── slack.go           # slack-botコマンド（新規）
│   └── vectorize.go       # vectorizeコマンド
├── internal/              # 内部ライブラリ
│   ├── config/           # 設定管理
│   ├── embedding/        # Embedding生成
│   ├── s3vector/         # S3 Vector統合
│   ├── opensearch/       # OpenSearch統合
│   └── vectorizer/       # ベクトル化サービス
├── markdown/             # Markdownドキュメント（使用前に準備）
├── export/               # Kibela用エクスポートツール（別ツール）
├── .envrc                # direnv設定
├── .env                  # 環境変数ファイル
└── CLAUDE.md            # Claude Code設定
```

## 依存関係

### 主要なライブラリ

- **github.com/spf13/cobra**: CLIフレームワーク
- **github.com/joho/godotenv**: 環境変数読み込み
- **github.com/aws/aws-sdk-go-v2**: AWS SDK v2
  - S3サービス
  - S3 Vectors
  - Bedrock Runtime（Titan Embeddings）
- **gopkg.in/yaml.v3**: YAML処理

### AWS関連ライブラリ

- `github.com/aws/aws-sdk-go-v2/config`: AWS設定管理
- `github.com/aws/aws-sdk-go-v2/service/s3`: S3操作
- `github.com/aws/aws-sdk-go-v2/service/s3vectors`: S3 Vector操作
- `github.com/aws/aws-sdk-go-v2/service/bedrockruntime`: Bedrock Runtime操作

## 典型的なワークフロー

1. **初期設定**
   ```bash
   # 環境変数設定
   cp .env.example .env
   # .envファイルを編集
   ```

2. **Markdownドキュメントの準備**
   ```bash
   # markdownディレクトリを作成（存在しない場合）
   mkdir -p markdown
   
   # markdownファイルをディレクトリに配置
   # または、Kibelaノート用のエクスポートツールを使用：
   cd export
   go build -o RAGent-export
   ./RAGent-export
   cd ..
   ```

3. **ベクトル化とS3保存**
   ```bash
   # ドライランで確認
   RAGent vectorize --dry-run
   
   # 実際のベクトル化実行
   RAGent vectorize

   # フォローモードで継続的にベクトル化（デフォルト30分間隔）
   RAGent vectorize --follow

   # フォローモードの間隔をカスタマイズ（例: 15分間隔）
   RAGent vectorize --follow --interval 15m
   ```

   > メモ: `--follow` は `--dry-run` および `--clear` と併用できません。

4. **ベクトルデータの確認**
   ```bash
   RAGent list
   ```

5. **セマンティック検索の実行**
   ```bash
   RAGent query -q "検索したい内容"
   ```

## トラブルシューティング

### よくあるエラー

1. **環境変数が設定されていない**
   ```
   Error: required environment variable not set
   ```
   → `.env`ファイルが正しく設定されているか確認

2. **設定エラー**
   ```
   Error: configuration not found or invalid
   ```
   → 設定と認証情報が正しいか確認

3. **AWS認証エラー**
   ```
   Error: AWS credentials not found
   ```
   → AWS認証情報が正しく設定されているか確認

4. **S3 Vector Index not found**
   ```
   Error: vector index not found
   ```
   → S3 Vector Indexが作成されているか確認

5. **Slackトークン未設定または無効**
   ```
   Slack search unavailable: SLACK_BOT_TOKEN not configured
   ```
   → `SLACK_SEARCH_ENABLED=true` と同じワークスペースの `SLACK_BOT_TOKEN` が設定されているか確認してください。

6. **Slackレート制限 (HTTP 429)**
   ```
   slack search failed: rate_limited
   ```
   → `Retry-After` ヘッダーに従い、`SLACK_SEARCH_MAX_RETRIES` を増やすか `SLACK_SEARCH_MAX_RESULTS` を減らしてください。

7. **Botがチャンネルに参加していない**
   ```
   slack search failed: not_in_channel
   ```
   → Botユーザーを対象チャンネルに招待するか、`--slack-channels` から当該チャンネルを除外してください。

### デバッグ方法

```bash
# 詳細ログ付きで実行
RAGent vectorize --dry-run

# 環境変数の確認
env | grep AWS
```

## ライセンス

このプロジェクトのライセンス情報については、リポジトリのLICENSEファイルを参照してください。

## OpenSearch RAG設定

### AWS OpenSearchのロールマッピング

AWS OpenSearchでIAM認証を使用する場合、IAMロールがOpenSearchクラスターにアクセスできるようにロールマッピングを設定する必要があります。

#### 現在のロールマッピングを確認
```bash
curl -u "master_user:master_pass" -X GET \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/all_access"
```

#### IAMロールをOpenSearchロールにマッピング
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

#### RAG操作用のカスタムロールを作成
```bash
# 必要な権限を持つカスタムロールを作成
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

# IAMロールをカスタムロールにマッピング
curl -u "master_user:master_pass" -X PUT \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/RAGent_role" \
  -H "Content-Type: application/json" \
  -d '{
    "backend_roles": ["arn:aws:iam::123456789012:role/your-iam-role"],
    "hosts": [],
    "users": []
  }'
```

### ハイブリッド検索の設定

最適なRAGパフォーマンスのために、適切な重みでハイブリッド検索を設定します：

- **一般的な検索**: BM25重み: 0.5、ベクトル重み: 0.5
- **キーワード重視**: BM25重み: 0.7、ベクトル重み: 0.3
- **セマンティック重視**: BM25重み: 0.3、ベクトル重み: 0.7

#### 日本語文書の推奨設定
- BM25演算子: "or"（デフォルト）
- BM25最小一致数: 精度向上のために"2"または"70%"
- 日本語NLP使用: true（kuromojiトークナイザーを有効化）

## セットアップ自動化（setup.sh）

AWS OpenSearch のセキュリティ設定（ドメインアクセス・ロール・ロールマッピング）、RAG用インデックス作成、Bedrock/S3 Vectors の IAM 権限付与を対話式に一括実行するスクリプトです。OpenSearch Security API への呼び出しは SigV4 署名で行います。

前提条件
- AWS CLI v2 が設定済み（対象ドメイン/IAM を更新できる権限）
- OpenSearch ドメインへ到達可能（VPC エンドポイントへ直接、または `https://localhost:9200` へのポートフォワード）

実行
```bash
bash setup.sh
```

入力される内容（対話）
- AWSアカウントID、OpenSearchドメイン/リージョン、エンドポイント利用（直アクセス or localhost:9200）、IAMロールARN（RAG実行ロール、必要なら管理ロール）、インデックス名、S3 Vectors バケット/インデックス/リージョン、Bedrock リージョンとモデルID

実行される内容
- ドメインのアクセスポリシー更新（指定したIAMロールを許可）
- （任意）Advanced Security の MasterUserARN 設定
- OpenSearch ロール `kibela_rag_role` の作成/更新（クラスタヘルス + <index>* への CRUD/Bulk 権限）とロールマッピング
- 対象インデックスが無ければ作成（日本語 `kuromoji` + `knn_vector` 1024, lucene/cosinesimil）
- （任意）トラブルシュート用に RAGロールを `all_access` に一時マッピング
- RAGロールへ Bedrock InvokeModel と S3 Vectors（指定の bucket/index）用の IAM インラインポリシーを付与

注意事項
- ポートフォワードを使う場合は、本スクリプトが Host ヘッダを VPC ドメインへ設定するため `https://localhost:9200` でも SigV4 検証が通ります。
- `all_access` 付与は検証用の一時措置です。検証完了後は除去してください。

## MCPサーバー統合

### Claude Desktop設定

MCPサーバーを設定し、認証を完了した後、Claude Desktop設定にサーバーを追加します:

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

SSE クライアント（例: `claude mcp add --transport sse ...`）は `/mcp` ではなく専用の `/sse` エンドポイントを指定してください。

### 利用可能MCPツール

- **ragent-hybrid_search**: BM25とベクトル検索を使用したハイブリッド検索の実行
  - パラメータ: `query`, `max_results`, `bm25_weight`, `vector_weight`, `use_japanese_nlp`
  - 戻り値: スコアと参照情報を含む構造化された検索結果

### 認証フロー

1. MCPサーバーを起動: `RAGent mcp-server --auth-method oidc`
2. 認証URLにアクセス: `http://localhost:8080/login`
3. アイデンティティプロバイダーOAuth2フローを完了
4. 提供されたClaude Desktop設定をコピー
5. Claude Desktop設定に設定を追加

![OIDC Authentication](doc/oidc.png)

## ライセンス

このプロジェクトのライセンス情報については、リポジトリのLICENSEファイルを参照してください。

## 貢献

プロジェクトへの貢献を歓迎します。Issue報告やPull Requestをお気軽にお送りください。
### 5. slack-bot - Slack Bot（メンションでRAG検索）

Slack の Bot を起動し、メンションで質問された内容に対して RAG 検索結果を Block Kit で返信します。

```bash
RAGent slack-bot
```

要件:
- `SLACK_BOT_TOKEN` を設定（`.env.example` 参照）。
- Bot を対象チャンネルへ招待。
- スレッド返信を有効化する場合は `SLACK_ENABLE_THREADING=true`。
- スレッドコンテキスト機能を使う場合は `SLACK_THREAD_CONTEXT_ENABLED=true`（デフォルト）を維持し、履歴の取得件数は `SLACK_THREAD_CONTEXT_MAX_MESSAGES`（デフォルト10件）で調整。
- レート制限は `SLACK_RATE_USER_PER_MINUTE`/`SLACK_RATE_CHANNEL_PER_MINUTE`/`SLACK_RATE_GLOBAL_PER_MINUTE` で調整。
- Slack検索を併用する場合は `SLACK_SEARCH_ENABLED=true` を設定し、`SLACK_SEARCH_MAX_RESULTS` や `SLACK_SEARCH_MAX_CONTEXT_MESSAGES` で取得件数を調整してください。
- OpenSearch の設定（`OPENSEARCH_ENDPOINT`、`OPENSEARCH_INDEX`、`OPENSEARCH_REGION`）が必須です。Slack Bot では S3 Vector へのフォールバックは使用しません。

詳細は `docs/slack-bot.md` を参照してください。

Slack検索が有効な場合、返信メッセージに **Conversations from Slack** セクションが挿入され、各メッセージへのパーマリンクから元スレッドに素早く移動できます。

### 6. mcp-server - Claude Desktop統合用MCPサーバー（新機能）

Claude Desktopやその他のMCP対応ツールにハイブリッド検索機能を提供するMCP（Model Context Protocol）サーバーを起動します。

```bash
# OIDC認証のみで開始
RAGent mcp-server --auth-method oidc

# IP認証またはOIDC認証のいずれかを許可（開発時推奨）
RAGent mcp-server --auth-method either

# IP認証とOIDC認証の両方を要求（最高セキュリティ）
RAGent mcp-server --auth-method both

# IP認証のみ（デフォルト）
RAGent mcp-server --auth-method ip
```

`SLACK_SEARCH_ENABLED=true` の場合、MCPツール `ragent-hybrid_search` は以下のパラメータを追加で受け付けます:

- `enable_slack_search` (bool, デフォルト `false`): リクエスト単位でSlack検索を有効化。
- `slack_channels` (string[]): Slack検索対象チャンネル名の配列（省略可）。

レスポンスには `slack_results` フィールドが含まれ、メッセージのメタデータとパーマリンクをMCPクライアント側でレンダリングできます。

**認証方式:**
- `ip`: 従来のIPアドレスベース認証のみ
- `oidc`: OpenID Connect認証のみ
- `both`: IP認証とOIDC認証の両方を要求
- `either`: IP認証またはOIDC認証のいずれかを許可

**認証バイパス設定:**
CI/CD環境や社内ネットワーク向けに、特定のIPレンジからのアクセスでは認証をスキップできます:
```bash
# 特定IPレンジをバイパス
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --bypass-ip-range "172.16.0.0/12"

# バイパスアクセスを監査ログに記録
RAGent mcp-server --bypass-ip-range "10.10.0.0/16" --bypass-audit-log

# トラブルシューティング用の詳細ログ
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --bypass-verbose-log

# 信頼できるプロキシを指定してX-Forwarded-Forを処理
RAGent mcp-server --bypass-ip-range "10.0.0.0/8" --trusted-proxies "192.168.1.1"
```

**対応OIDC プロバイダー:**
- Google Workspace (`https://accounts.google.com`)
- Microsoft Azure AD/Entra ID (`https://login.microsoftonline.com/{tenant}/v2.0`)
- Okta (`https://{domain}.okta.com`)
- Keycloak (`https://{server}/realms/{realm}`)
- カスタムOAuth2プロバイダー

**機能:**
- JSON-RPC 2.0準拠のMCPプロトコル
- ハイブリッド検索ツール: `ragent-hybrid_search`
- 複数の認証方式
- Claude Desktop統合
- SSEおよびHTTPトランスポート対応
- ブラウザベースの認証フロー

**要件:**
- MCPサーバー機能にはOpenSearch設定が必要
- OIDC用: OAuth2アプリケーションの設定と環境変数の設定
- IP認証用: 許可IPアドレスまたは範囲の設定

**Claude Desktopでの使用:**
認証後、提供されるコマンドを使用してClaude DesktopにサーバーVを追加:
```bash
claude mcp add --transport sse ragent https://your-server.example.com/sse --header "Authorization: Bearer <JWT>"
```

詳細: `doc/mcp-server.md` および `doc/oidc-authentication.md` を参照してください。
