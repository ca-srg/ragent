# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

kiberag は Kibela GraphQL API から全てのノートを取得し、適切なメタデータを付与してmarkdownファイルとしてエクスポートし、Amazon S3 VectorsへのベクトルデータKHRA作成によるRAGシステムです。ノートのエクスポート、ベクトル化、セマンティック検索の機能を提供します。

## Architecture

### Core Structure
- **main.go**: エントリーポイント、cobra CLIのExecuteを呼び出し
- **cmd/**: CLIコマンド定義
  - `root.go`: ルートコマンドと共通設定
  - `export.go`: exportコマンドの実装（Kibela APIからのノート取得）
  - `vectorize.go`: vectorizeコマンドの実装（ベクトル化とS3保存）
  - `query.go`: queryコマンドの実装（セマンティック検索）
  - `list.go`: listコマンドの実装（ベクトル一覧表示）

### Internal Packages
- **internal/kibera/**: Kibela GraphQL APIクライアント
  - データ構造体（Note, Author, Group, Folder等）
  - GetAllNotes、fetchNotesメソッドによるAPI通信
- **internal/export/**: エクスポート機能
  - ノートのmarkdown変換とファイル保存
  - ファイル名生成、カテゴリ抽出機能
- **internal/vectorizer/**: ベクトル化サービス
  - VectorizerService: 並行処理によるベクトル化
  - ProcessingStats: 処理統計管理
  - エラーハンドリングとドライラン機能
- **internal/embedding/**: 埋め込み生成
  - `bedrock/`: Amazon Bedrock統合（Voyage AI）
  - `voyage/`: Voyage AI直接統合
  - 複数プロバイダー対応アーキテクチャ
- **internal/s3vector/**: S3 Vector統合
  - ベクトルストレージとインデックス管理
  - メタデータ付きベクトル保存
  - セマンティック検索機能
- **internal/config/**: 設定管理
  - YAML設定ファイル読み込み
  - 環境変数とのマージ
  - 設定検証とデフォルト値
- **internal/scanner/**: ファイルスキャナー
  - markdownファイルの再帰的発見
  - ファイルフィルタリング機能
- **internal/metadata/**: メタデータ抽出
  - FrontMatter解析
  - ファイル情報抽出
- **internal/filter/**: フィルタ機能
  - RAG検索時の除外フィルタロジック
  - S3 Vector対応フィルタ構築
  - ユーザーフィルタとの統合機能
- **internal/types/**: 共通型定義
  - システム全体で使用される構造体

### Output Directories
- **markdown/**: エクスポートされたmarkdownファイルの出力先
- **doc/**: プロジェクト文書（S3 Vector設定推奨など）
- **reference/**: 参考実装とサンプルコード

## Required Environment Variables

環境変数は `.env` ファイルで設定され、direnv (`.envrc`) により自動ロードされます:

### Kibela API設定
- `KIBELA_TOKEN`: Kibela APIアクセストークン
- `KIBELA_TEAM`: 対象チーム名

### AWS設定
- `AWS_REGION`: AWSリージョン
- `AWS_ACCESS_KEY_ID`: AWS アクセスキーID
- `AWS_SECRET_ACCESS_KEY`: AWS シークレットアクセスキー

### S3 Vector設定
- `S3_VECTOR_INDEX_NAME`: S3 Vector インデックス名
- `S3_BUCKET_NAME`: S3バケット名

### Voyage AI設定
- `VOYAGE_API_KEY`: Voyage APIキー

### フィルタ設定
- `EXCLUDE_CATEGORIES`: RAG検索で除外するカテゴリ（カンマ区切り、デフォルト: "個人メモ,日報"）

## Development Commands

```bash
# 依存関係の管理
go mod tidy
go mod download

# ビルド
go build -o kiberag

# テスト実行
go test ./...

# フォーマット
go fmt ./...

# 各コマンドの実行例
go run main.go export                    # ノートエクスポート
go run main.go vectorize --dry-run       # ベクトル化（ドライラン）
go run main.go vectorize                 # ベクトル化実行
go run main.go query -q "検索クエリ"      # セマンティック検索
go run main.go list                      # ベクトル一覧表示

# ベンダリング（禁止されている）
# go mod vendor は使用しない
```

## Usage Examples

```bash
# 1. 全ノートをエクスポート
./kiberag export

# 2. ベクトル化とS3保存
./kiberag vectorize --directory ./markdown --concurrency 10

# 3. セマンティック検索
./kiberag query -q "機械学習のアルゴリズム" --top-k 5

# 4. ベクトル一覧表示
./kiberag list --prefix "docs/"
```

## Dependencies

### Core Framework
- `github.com/spf13/cobra`: CLIフレームワーク
- `github.com/machinebox/graphql`: GraphQLクライアント
- `github.com/joho/godotenv`: 環境変数読み込み
- `gopkg.in/yaml.v3`: YAML設定ファイル処理

### AWS Integration
- `github.com/aws/aws-sdk-go-v2`: AWS SDK v2
- `github.com/aws/aws-sdk-go-v2/config`: AWS設定管理
- `github.com/aws/aws-sdk-go-v2/service/s3`: S3操作
- `github.com/aws/aws-sdk-go-v2/service/s3vectors`: S3 Vector操作
- `github.com/aws/aws-sdk-go-v2/service/bedrockruntime`: Bedrock Runtime操作

## Configuration

設定は以下の優先順位で読み込まれます：
1. 環境変数
2. `config.yaml` ファイル
3. デフォルト値

推奨されるS3 Vector設定：
- ディメンション: 1024次元（Voyage-3-large）
- 距離メトリック: コサイン距離
- 対象: 6,583個の日本語ビジネス文書