# kiberag - Kibela API Gateway

**[English README](README.md)**

kiberag は Kibela GraphQL API から全てのノートを取得し、適切なメタデータを付与してmarkdownファイルとしてエクスポートするCLIツールです。さらに、Amazon S3 VectorsへのベクトルデータKHRA作成でRAGシステムとして利用することを目的としています。

## 機能

- **ノートエクスポート**: Kibela GraphQL APIから全てのノートを取得してmarkdownファイルとして保存
- **ベクトル化**: markdownファイルをAmazon Bedrockを使用してembeddingに変換
- **S3 Vector統合**: 生成されたベクトルをAmazon S3 Vectorsに保存
- **セマンティック検索**: S3 Vector Indexを使用したセマンティック類似性検索
- **ベクトル管理**: S3に保存されたベクトルの一覧表示

## 必要な環境変数

プロジェクトルートに `.env` ファイルを作成し、以下の環境変数を設定してください：

```env
# Kibela API設定
KIBELA_TOKEN=your_kibela_api_token
KIBELA_TEAM=your_team_name

# AWS設定
AWS_REGION=your_aws_region
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# S3 Vector設定
S3_VECTOR_INDEX_NAME=your_vector_index_name
S3_BUCKET_NAME=your_s3_bucket_name

```

## インストール

### 前提条件

- Go 1.25.0以上
- direnv（推奨）

### ビルド

```bash
# リポジトリをクローン
git clone https://github.com/rluisr/kiberag.git
cd kiberag

# 依存関係をインストール
go mod download

# ビルド
go build -o kiberag

# 実行可能ファイルをPATHに追加（オプション）
mv kiberag /usr/local/bin/
```

## コマンド一覧

### 1. export - ノートのエクスポート

Kibela GraphQL APIから全てのノートを取得し、markdownファイルとして `markdown/` ディレクトリに保存します。

```bash
kiberag export
```

**機能:**
- Kibela APIから全ノートを取得
- 適切なメタデータを付与
- ファイル名の自動生成
- カテゴリの自動抽出
- `markdown/` ディレクトリへの保存

### 2. vectorize - ベクトル化とS3保存

markdownファイルを読み込み、メタデータを抽出し、Amazon Bedrockを使用してembeddingを生成してAmazon S3 Vectorsに保存します。

```bash
kiberag vectorize
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

### 3. query - セマンティック検索

S3 Vector Indexに対してセマンティック類似性検索を実行します。

```bash
# 基本的な検索
kiberag query -q "machine learning algorithms"

# 詳細オプション付きの検索
kiberag query --query "API documentation" --top-k 5 --json

# メタデータフィルター付きの検索
kiberag query -q "error handling" --filter '{"category":"programming"}'
```

**オプション:**
- `-q, --query`: 検索クエリテキスト（必須）
- `-k, --top-k`: 返される類似結果の数（デフォルト: 10）
- `-j, --json`: 結果をJSON形式で出力
- `-f, --filter`: JSONメタデータフィルター（例: `'{"category":"docs"}'`）

**使用例:**
```bash
# 技術文書の検索
kiberag query -q "Docker コンテナ設定" --top-k 3

# 特定カテゴリでの検索
kiberag query -q "authentication" --filter '{"type":"security"}' --json

# より多くの結果を取得
kiberag query -q "database optimization" --top-k 20
```

### 4. list - ベクトル一覧表示

S3 Vector Indexに保存されているベクトルの一覧を表示します。

```bash
# 全ベクトルを表示
kiberag list

# プレフィックスでフィルタリング
kiberag list --prefix "docs/"
```

**オプション:**
- `-p, --prefix`: ベクトルキーをフィルタリングするプレフィックス

**機能:**
- 保存されたベクトルキーの表示
- プレフィックスによるフィルタリング
- ベクトルデータベースの内容確認

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
kiberag/
├── main.go                 # エントリーポイント
├── cmd/                    # CLIコマンド定義
│   ├── root.go            # ルートコマンドと共通設定
│   ├── export.go          # exportコマンド
│   ├── query.go           # queryコマンド
│   ├── list.go            # listコマンド
│   └── vectorize.go       # vectorizeコマンド
├── internal/              # 内部ライブラリ
│   ├── kibera/           # Kibela GraphQL APIクライアント
│   └── export/           # エクスポート機能
├── markdown/             # エクスポートされたmarkdownファイル
├── .envrc                # direnv設定
├── .env                  # 環境変数ファイル
└── CLAUDE.md            # Claude Code設定
```

## 依存関係

### 主要なライブラリ

- **github.com/spf13/cobra**: CLIフレームワーク
- **github.com/machinebox/graphql**: GraphQLクライアント
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

2. **ノートのエクスポート**
   ```bash
   kiberag export
   ```

3. **ベクトル化とS3保存**
   ```bash
   # ドライランで確認
   kiberag vectorize --dry-run
   
   # 実際のベクトル化実行
   kiberag vectorize
   ```

4. **ベクトルデータの確認**
   ```bash
   kiberag list
   ```

5. **セマンティック検索の実行**
   ```bash
   kiberag query -q "検索したい内容"
   ```

## トラブルシューティング

### よくあるエラー

1. **環境変数が設定されていない**
   ```
   Error: required environment variable not set
   ```
   → `.env`ファイルが正しく設定されているか確認

2. **Kibela API接続エラー**
   ```
   Error: failed to connect to Kibela API
   ```
   → `KIBELA_TOKEN`と`KIBELA_TEAM`が正しいか確認

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
kiberag vectorize --dry-run

# 環境変数の確認
env | grep KIBERA
env | grep AWS
```

## ライセンス

このプロジェクトのライセンス情報については、リポジトリのLICENSEファイルを参照してください。

## 貢献

プロジェクトへの貢献を歓迎します。Issue報告やPull Requestをお気軽にお送りください。