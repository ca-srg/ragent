# mdRAG - Markdownドキュメント用RAGシステム構築ツール

**[English README](README.md)**

mdRAG は、Markdownドキュメントからハイブリッド検索（BM25 + ベクトル検索）を利用したRAG（Retrieval-Augmented Generation）システムを構築するCLIツールです。Amazon S3 VectorsとOpenSearchを活用した高精度な検索機能を提供します。

## 機能

- **ベクトル化**: markdownファイルをAmazon Bedrockを使用してembeddingに変換
- **S3 Vector統合**: 生成されたベクトルをAmazon S3 Vectorsに保存
- **ハイブリッド検索**: OpenSearchを使用したBM25 + ベクトル検索の組み合わせ
- **セマンティック検索**: S3 Vector Indexを使用したセマンティック類似性検索
- **対話型RAGチャット**: コンテキスト認識応答を行うチャットインターフェース
- **ベクトル管理**: S3に保存されたベクトルの一覧表示

## 前提条件

### Markdownドキュメントの準備

mdRAGを使用する前に、`markdown/` ディレクトリにMarkdownドキュメントを準備する必要があります。これらのドキュメントがRAGシステムで検索可能なコンテンツとなります。

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
```

## インストール

### 前提条件

- Go 1.25.0以上
- direnv（推奨）

### ビルド

```bash
# リポジトリをクローン
git clone https://github.com/ca-srg/mdrag.git
cd mdRAG

# 依存関係をインストール
go mod download

# ビルド
go build -o mdRAG

# 実行可能ファイルをPATHに追加（オプション）
mv mdRAG /usr/local/bin/
```

## コマンド一覧

### 1. vectorize - ベクトル化とS3保存

markdownファイルを読み込み、メタデータを抽出し、Amazon Bedrockを使用してembeddingを生成してAmazon S3 Vectorsに保存します。

```bash
mdRAG vectorize
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
mdRAG query -q "machine learning algorithms"

# 詳細オプション付きの検索
mdRAG query --query "API documentation" --top-k 5 --json

# メタデータフィルター付きの検索
mdRAG query -q "error handling" --filter '{"category":"programming"}'
```

**オプション:**
- `-q, --query`: 検索クエリテキスト（必須）
- `-k, --top-k`: 返される類似結果の数（デフォルト: 10）
- `-j, --json`: 結果をJSON形式で出力
- `-f, --filter`: JSONメタデータフィルター（例: `'{"category":"docs"}'`）

**使用例:**
```bash
# 技術文書の検索
mdRAG query -q "Docker コンテナ設定" --top-k 3

# 特定カテゴリでの検索
mdRAG query -q "authentication" --filter '{"type":"security"}' --json

# より多くの結果を取得
mdRAG query -q "database optimization" --top-k 20
```

### 3. list - ベクトル一覧表示

S3 Vector Indexに保存されているベクトルの一覧を表示します。

```bash
# 全ベクトルを表示
mdRAG list

# プレフィックスでフィルタリング
mdRAG list --prefix "docs/"
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
mdRAG chat

# カスタムコンテキストサイズでチャット
mdRAG chat --context-size 10

# ハイブリッド検索の重みバランスをカスタマイズ
mdRAG chat --bm25-weight 0.7 --vector-weight 0.3

# カスタムシステムプロンプトでチャット
mdRAG chat --system "あなたはドキュメントに特化した親切なアシスタントです。"
```

**オプション:**
- `-c, --context-size`: 取得するコンテキストドキュメント数（デフォルト: 5）
- `-i, --interactive`: 対話モードで実行（デフォルト: true）
- `-s, --system`: チャット用のシステムプロンプト
- `-b, --bm25-weight`: ハイブリッド検索でのBM25スコアリングの重み（0-1、デフォルト: 0.5）
- `-v, --vector-weight`: ハイブリッド検索でのベクトルスコアリングの重み（0-1、デフォルト: 0.5）
- `--use-japanese-nlp`: OpenSearchで日本語NLP最適化を使用（デフォルト: true）

**機能:**
- BM25とベクトル類似性を組み合わせたハイブリッド検索
- OpenSearchが利用できない場合のS3 Vectorへの自動フォールバック
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
mdRAG/
├── main.go                 # エントリーポイント
├── cmd/                    # CLIコマンド定義
│   ├── root.go            # ルートコマンドと共通設定
│   ├── query.go           # queryコマンド
│   ├── list.go            # listコマンド
│   ├── chat.go            # chatコマンド
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
   go build -o mdRAG-export
   ./mdRAG-export
   cd ..
   ```

3. **ベクトル化とS3保存**
   ```bash
   # ドライランで確認
   mdRAG vectorize --dry-run
   
   # 実際のベクトル化実行
   mdRAG vectorize
   ```

4. **ベクトルデータの確認**
   ```bash
   mdRAG list
   ```

5. **セマンティック検索の実行**
   ```bash
   mdRAG query -q "検索したい内容"
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

### デバッグ方法

```bash
# 詳細ログ付きで実行
mdRAG vectorize --dry-run

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

# IAMロールをカスタムロールにマッピング
curl -u "master_user:master_pass" -X PUT \
  "https://your-opensearch-endpoint/_plugins/_security/api/rolesmapping/mdRAG_role" \
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

## ライセンス

このプロジェクトのライセンス情報については、リポジトリのLICENSEファイルを参照してください。

## 貢献

プロジェクトへの貢献を歓迎します。Issue報告やPull Requestをお気軽にお送りください。
