# MCPサーバー セットアップガイド

## 概要

RAGentのMCP（Model Context Protocol）サーバー機能は、ハイブリッド検索（BM25 + ベクトル検索）をMCPプロトコル経由で外部アプリケーションに提供します。Claude Desktop、IDEs、その他のMCP対応ツールからRAGentの高精度検索機能を利用できます。

**⚠️ 重要**: バージョン2.0以降、RAGentはMCP公式SDK v0.4.0ベースに移行しました。従来のカスタム実装から公式SDKへのマイグレーションにより、プロトコル準拠性と保守性が向上しています。

### 主な特徴

- **公式SDK対応**: MCP SDK v0.4.0による標準準拠実装
- **ハイブリッド検索**: BM25とベクトル検索を融合した高精度検索
- **IPアドレス認証**: 企業レベルのセキュリティ機能
- **JSON-RPC 2.0準拠**: MCP標準プロトコルに完全対応
- **日本語最適化**: kuromoji tokenizerによる日本語文書検索
- **既存機能再利用**: chatコマンドと同等の検索品質
- **後方互換性**: 既存のMCPクライアント統合を維持

## SDK マイグレーションガイド

### v2.0 での変更点

RAGent v2.0では、カスタムMCP実装から公式`modelcontextprotocol/go-sdk` v0.4.0への移行を実施しました：

#### 技術的変更
- **プロトコル実装**: 公式SDKによるMCP準拠実装
- **型システム**: SDK標準型による型安全性向上
- **エラーハンドリング**: 標準MCPエラーコードとメッセージ
- **パフォーマンス**: 最適化されたSDKパフォーマンス

#### ユーザー影響
- **設定**: 既存の環境変数設定は完全互換
- **API**: ツール呼び出しインターフェースは変更なし
- **機能**: 全ての検索機能は同じ動作を維持
- **クライアント**: 既存のMCPクライアント統合は無変更で動作

### マイグレーション手順

#### 1. バージョン確認
```bash
# 現在のバージョンを確認
./ragent --version

# v2.0以上であることを確認
RAGent version 2.0.0 (MCP SDK v0.4.0)
```

#### 2. 設定検証
```bash
# SDK互換性チェックを実行
./ragent mcp-server --validate-config

# 出力例：
# ✅ SDK compatibility validation passed
# ✅ All configuration options are compatible
# ✅ Server ready for SDK-based operation
```

#### 3. 動作確認
```bash
# サーバー起動テスト
./ragent mcp-server --test-startup

# 既存クライアントとの互換性テスト
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":"test"}' \
  http://localhost:8080
```

#### 4. パフォーマンス検証
```bash
# パフォーマンステストの実行
go test ./tests/benchmarks -bench=BenchmarkSDK -run=^$

# 期待される結果：
# - 起動時間: <500ms
# - リクエストレイテンシ: 従来と同等
# - メモリ使用量: 20%以内の増加
```

### 移行時のトラブルシューティング

#### 設定関連のエラー
```bash
# エラー: SDK configuration validation failed
# 解決策: 設定値の範囲確認
MCP_DEFAULT_TIMEOUT_SECONDS=600  # 最大600秒
MCP_DEFAULT_SEARCH_SIZE=1000     # 最大1000件
```

#### 型互換性エラー
```bash
# エラー: tool parameter type mismatch
# 解決策: JSON型の明示的指定
{
  "query": "検索文字列",          // string
  "max_results": 10,            // integer (not string)
  "bm25_weight": 0.7,          // float (not string)
  "use_japanese_nlp": true     // boolean (not string)
}
```

#### プロトコル準拠エラー
```bash
# エラー: invalid JSON-RPC 2.0 format
# 解決策: 必須フィールドの確認
{
  "jsonrpc": "2.0",           // 必須: 正確なバージョン
  "method": "tools/call",     // 必須: メソッド名
  "params": {...},            // 必須: パラメータ
  "id": "unique_id"           // 必須: リクエストID
}
```

## 前提条件

MCPサーバーを使用するには、以下の設定が完了している必要があります：

1. **OpenSearch環境**: ハイブリッド検索用のOpenSearchクラスター
2. **AWS Bedrock**: ベクトル埋め込み生成用の設定
3. **ベクトル化済み文書**: `ragent vectorize`コマンドでのドキュメント処理完了

## 環境変数設定

### 必須環境変数

```bash
# AWS設定
AWS_REGION=ap-northeast-1
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key

# OpenSearch設定
OPENSEARCH_ENDPOINT=https://your-opensearch-endpoint.region.es.amazonaws.com
OPENSEARCH_USERNAME=your_username  # オプション
OPENSEARCH_PASSWORD=your_password  # オプション

# S3 Vector設定
S3_VECTOR_INDEX_NAME=ragent-docs
S3_BUCKET_NAME=your-vector-bucket
```

### MCPサーバー専用設定

```bash
# MCPサーバー基本設定
MCP_SERVER_HOST=localhost
MCP_SERVER_PORT=8080
MCP_TOOL_NAME=hybrid_search  # ツール名（デフォルト: hybrid_search）
MCP_TOOL_PREFIX=ragent-      # ツール名プレフィックス（デフォルト: ragent-）

# IPアドレス認証設定
MCP_IP_AUTH_ENABLED=true
MCP_ALLOWED_IPS=127.0.0.1,::1,192.168.1.0/24  # カンマ区切り

# 検索デフォルト設定
MCP_DEFAULT_INDEX_NAME=ragent-docs
MCP_DEFAULT_SEARCH_SIZE=10
MCP_DEFAULT_BM25_WEIGHT=0.5
MCP_DEFAULT_VECTOR_WEIGHT=0.5
MCP_DEFAULT_USE_JAPANESE_NLP=true
MCP_DEFAULT_TIMEOUT_SECONDS=30

# サーバー詳細設定
MCP_SERVER_READ_TIMEOUT=30s
MCP_SERVER_WRITE_TIMEOUT=30s
MCP_SERVER_IDLE_TIMEOUT=120s
MCP_SERVER_MAX_HEADER_BYTES=1048576
MCP_SERVER_GRACEFUL_SHUTDOWN=true
MCP_SERVER_SHUTDOWN_TIMEOUT=30s
# アクセスログは常時有効で、HTTPメソッドとIP情報を自動で出力します

# SDK専用設定（v2.0以降）
MCP_SDK_VERSION=v0.4.0              # SDKバージョン（情報表示用）
MCP_SDK_STRICT_VALIDATION=true      # 厳密なプロトコル検証
MCP_SDK_ENABLE_METRICS=false        # SDKメトリクス収集（オプション）
MCP_SDK_LOG_PROTOCOL_ERRORS=true    # プロトコルエラーの詳細ログ
```

## 起動方法

### 基本起動

```bash
# デフォルト設定で起動
./ragent mcp-server

# カスタムポートで起動
./ragent mcp-server --port 9000

# 特定IPレンジを許可
./ragent mcp-server --allowed-ips "192.168.1.0/24,10.0.0.0/8"
```

### 高度な設定例

```bash
# セキュリティを重視した本番環境設定
./ragent mcp-server \
  --host 0.0.0.0 \
  --port 8080 \
  --enable-ip-auth \
  --allowed-ips "1.1.1.1,1.1.1.2,1.1.1.3" \

# 開発環境設定（セキュリティ緩和）
./ragent mcp-server \
  --host localhost \
  --port 3000 \
  --disable-ip-auth
```

## MCPクライアント統合

### Claude Desktop設定

`~/.claude_desktop_config.json`に以下を追加：

```json
{
  "mcpServers": {
    "ragent": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-d", "@-",
        "http://localhost:8080/mcp"
      ],
      "env": {}
    }
  }
}
```

SSE ベースの MCP クライアントを追加する場合は `https://.../sse` を指定し、`claude mcp add --transport sse` のように `/mcp` ではなく専用エンドポイントを利用してください。

### プログラム統合例

```python
import json
import requests

# MCP JSON-RPC リクエスト
payload = {
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
        "name": "ragent-hybrid_search",
        "arguments": {
            "query": "機械学習のアルゴリズム",
            "max_results": 5,
            "bm25_weight": 0.7,
            "vector_weight": 0.3,
            "use_japanese_nlp": True
        }
    },
    "id": "1"
}

response = requests.post(
    "http://localhost:8080/mcp",
    headers={"Content-Type": "application/json"},
    json=payload
)

result = response.json()
print(json.dumps(result, ensure_ascii=False, indent=2))
```

## セキュリティ設定

### IPアドレス認証

推奨される本番環境でのIP制限設定：

```bash
# 環境変数での設定
MCP_IP_AUTH_ENABLED=true
MCP_ALLOWED_IPS=1.1.1.1,1.0.0.1

# コマンドラインでの設定
./ragent mcp-server --allowed-ips "1.1.1.1,1.0.0.1"
```

### セキュリティベストプラクティス

1. **本番環境では必ずIP認証を有効化**
   ```bash
   MCP_IP_AUTH_ENABLED=true
   ```

2. **最小権限の原則でIP範囲を制限**
   ```bash
   MCP_ALLOWED_IPS=your_office_ip,your_server_ip
   ```

3. **アクセスログの有効化**
   ```bash
   MCP_SERVER_ENABLE_ACCESS_LOGGING=true
   ```

4. **適切なタイムアウト設定**
   ```bash
   MCP_DEFAULT_TIMEOUT_SECONDS=30
   MCP_SERVER_READ_TIMEOUT=30s
   ```

## 使用例

### 基本的なハイブリッド検索

```bash
# MCPクライアント経由での検索例
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "ragent-hybrid_search",
    "arguments": {
      "query": "APIの設計パターン",
      "max_results": 10,
      "use_japanese_nlp": true
    }
  },
  "id": "search_001"
}
```

### 重み調整検索

```bash
# BM25を重視した検索（キーワード検索重視）
{
  "arguments": {
    "query": "RESTful API",
    "bm25_weight": 0.8,
    "vector_weight": 0.2,
    "max_results": 5
  }
}

# ベクトル検索を重視した検索（意味検索重視）
{
  "arguments": {
    "query": "機械学習の基本概念",
    "bm25_weight": 0.3,
    "vector_weight": 0.7,
    "max_results": 8
  }
}
```

## 監視とログ

### アクセスログ

MCPサーバーは詳細なアクセスログを出力します：

```bash
[MCP Server] 2024/01/15 10:30:45 Starting MCP server on localhost:8080
[MCP Server] 2024/01/15 10:30:45 IP authentication enabled for IPs: [127.0.0.1 ::1]
[MCP Server] 2024/01/15 10:30:45 OpenSearch connection established: https://your-opensearch.com
[MCP Server] 2024/01/15 10:30:45 Registered tool 'ragent-hybrid_search' with 1 total tools
[MCP Server] 2024/01/15 10:30:45 Available tools: [ragent-hybrid_search]
[MCP Server] 2024/01/15 10:31:23 REQUEST: POST /mcp from 127.0.0.1
[MCP Server] 2024/01/15 10:31:24 RESPONSE: 200 OK (1.2s)
```

### ヘルスチェック

サーバーの稼働状況確認：

```bash
# HTTPエンドポイントでのヘルスチェック
curl -X GET http://localhost:8080/health

# レスポンス例
{
  "status": "healthy",
  "opensearch": "connected",
  "bedrock": "available",
  "uptime": "1h23m45s"
}
```

## トラブルシューティング

### よくある問題と解決方法

#### 1. サーバー起動失敗

**問題**: OpenSearchへの接続エラー
```bash
Error: OpenSearch health check failed: connection refused
```

**解決方法**:
```bash
# 環境変数を確認
echo $OPENSEARCH_ENDPOINT
echo $OPENSEARCH_USERNAME

# 接続テスト
curl -u $OPENSEARCH_USERNAME:$OPENSEARCH_PASSWORD $OPENSEARCH_ENDPOINT/_cluster/health
```

#### 2. IP認証エラー

**問題**: 許可されていないIPからのアクセス
```bash
Error: IP address not allowed: 192.168.1.100
```

**解決方法**:
```bash
# 許可IPリストに追加
MCP_ALLOWED_IPS=127.0.0.1,::1,192.168.1.100

# または一時的にIP認証を無効化（開発時のみ）
./ragent mcp-server --disable-ip-auth
```

#### 3. 検索結果が返らない

**問題**: 空の検索結果または検索エラー

**解決方法**:
```bash
# インデックスの存在確認
curl -X GET "$OPENSEARCH_ENDPOINT/ragent-docs/_count"

# ベクトル化が完了しているか確認
./ragent list

# chatコマンドで同じクエリをテスト
./ragent chat
```

#### 4. パフォーマンス問題

**問題**: 検索レスポンスが遅い

**解決方法**:
```bash
# タイムアウト設定の調整
MCP_DEFAULT_TIMEOUT_SECONDS=60

# 検索サイズの縮小
MCP_DEFAULT_SEARCH_SIZE=5

# 重み調整でBM25を重視（高速）
{
  "bm25_weight": 0.8,
  "vector_weight": 0.2
}
```

#### 5. メモリ使用量問題

**問題**: メモリ使用量が多い

**解決方法**:
```bash
# 同時接続数の制限
MCP_SERVER_MAX_CONNECTIONS=10

# アイドルタイムアウトの短縮
MCP_SERVER_IDLE_TIMEOUT=60s

# 検索結果サイズの制限
MCP_DEFAULT_SEARCH_SIZE=5
```

#### 6. SDK関連問題

**問題**: SDK初期化エラー
```bash
Error: failed to initialize SDK server: invalid server configuration
```

**解決方法**:
```bash
# 設定の再検証
./ragent mcp-server --validate-config

# SDK互換性の確認
MCP_SDK_STRICT_VALIDATION=false ./ragent mcp-server

# デバッグモードでの起動
LOG_LEVEL=debug ./ragent mcp-server
```

**問題**: プロトコル準拠エラー
```bash
Error: JSON-RPC 2.0 protocol violation
```

**解決方法**:
```bash
# 厳密検証の一時無効化（開発時）
MCP_SDK_STRICT_VALIDATION=false

# プロトコルエラーログの有効化
MCP_SDK_LOG_PROTOCOL_ERRORS=true

# クライアント側でJSON-RPC 2.0形式を確認
{
  "jsonrpc": "2.0",     # 必須: 正確なバージョン文字列
  "method": "tools/call",
  "params": {...},
  "id": "request_id"    # 必須: 各リクエストで一意なID
}
```

**問題**: 型変換エラー
```bash
Error: tool argument type mismatch: expected integer, got string
```

**解決方法**:
```bash
# 正しい型での指定
{
  "arguments": {
    "query": "検索クエリ",           # string
    "max_results": 10,             # number (not "10")
    "bm25_weight": 0.7,           # number (not "0.7")
    "use_japanese_nlp": true      # boolean (not "true")
  }
}
```

**問題**: SDK バージョン不整合
```bash
Warning: SDK version mismatch detected
```

**解決方法**:
```bash
# SDKバージョンの確認
./ragent --version | grep SDK

# 期待される出力: MCP SDK v0.4.0

# 依存関係の再構築
go mod tidy
go mod download

# 再ビルド
go build -o ragent .
```

## API仕様

### ツール定義

```json
{
  "name": "ragent-hybrid_search",
  "description": "Execute hybrid search using BM25 and vector search",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Search query text"
      },
      "max_results": {
        "type": "integer",
        "description": "Maximum number of results (1-50)",
        "minimum": 1,
        "maximum": 50,
        "default": 10
      },
      "bm25_weight": {
        "type": "number",
        "description": "Weight for BM25 scoring (0-1)",
        "minimum": 0,
        "maximum": 1,
        "default": 0.5
      },
      "vector_weight": {
        "type": "number",
        "description": "Weight for vector scoring (0-1)",
        "minimum": 0,
        "maximum": 1,
        "default": 0.5
      },
      "use_japanese_nlp": {
        "type": "boolean",
        "description": "Enable Japanese NLP optimization",
        "default": true
      },
      "timeout_seconds": {
        "type": "integer",
        "description": "Search timeout in seconds",
        "minimum": 5,
        "maximum": 300,
        "default": 30
      }
    },
    "required": ["query"]
  }
}
```

### レスポンス形式

```json
{
  "jsonrpc": "2.0",
  "result": {
    "documents": [
      {
        "title": "文書タイトル",
        "content": "文書内容の抜粋...",
        "score": 0.8945,
        "reference": "path/to/document.md",
        "category": "技術文書"
      }
    ],
    "total_results": 25,
    "search_time_ms": 1234.5,
    "references": {
      "path/to/document.md": "Document Title"
    }
  },
  "id": "search_001"
}
```

`score` フィールドは BM25・ベクトル結果を融合した後の `FusedScore`（正規化済みのハイブリッドスコア）を表します。

## 本番環境デプロイ

### Dockerコンテナ化

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o ragent .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/ragent .
COPY --from=builder /app/.env.example .env
EXPOSE 8080
CMD ["./ragent", "mcp-server"]
```

### システムサービス設定

```ini
[Unit]
Description=RAGent MCP Server
After=network.target

[Service]
Type=simple
User=ragent
WorkingDirectory=/opt/ragent
ExecStart=/opt/ragent/ragent mcp-server --port 8080
Restart=always
RestartSec=5
EnvironmentFile=/opt/ragent/.env

[Install]
WantedBy=multi-user.target
```

## サポート

### ログレベル調整

```bash
# デバッグログの有効化
LOG_LEVEL=debug ./ragent mcp-server

# エラーログのみ
LOG_LEVEL=error ./ragent mcp-server
```

### パフォーマンス測定

```bash
# 詳細なタイミング情報を含める
MCP_ENABLE_TIMING_LOGS=true ./ragent mcp-server

# SDKパフォーマンスメトリクス（v2.0以降）
MCP_SDK_ENABLE_METRICS=true ./ragent mcp-server

# パフォーマンステストの実行
go test ./tests/benchmarks -bench=BenchmarkSDK -benchtime=10s
```

## SDK v0.4.0 の利点

### プロトコル準拠性
- **標準化**: 公式SDK による完全なMCP準拠
- **互換性**: 将来のMCPアップデートへの対応保証
- **検証**: 厳密なプロトコル検証による信頼性向上

### 保守性向上
- **コード品質**: 公式実装によるバグの削減
- **アップデート**: SDK更新による自動的な改善
- **サポート**: 公式ドキュメントとコミュニティサポート

### パフォーマンス
- **最適化**: SDK内での効率的な実装
- **メモリ管理**: 改善されたリソース管理
- **並行処理**: 向上した同時接続処理能力

### セキュリティ
- **標準実装**: セキュリティベストプラクティスの適用
- **脆弱性対応**: SDK経由での迅速なセキュリティ修正
- **監査**: 標準化されたセキュリティ監査対応

## 移行検証チェックリスト

### 基本動作確認
- [ ] サーバー起動が500ms以内で完了
- [ ] `tools/list` コマンドが正常に動作
- [ ] ハイブリッド検索機能が期待通りに動作
- [ ] IP認証が正常に機能（有効化時）
- [ ] グレースフルシャットダウンが動作

### パフォーマンス確認  
- [ ] リクエストレイテンシが従来比+50ms以内
- [ ] メモリ使用量が従来比+20%以内
- [ ] 同時接続処理性能が維持されている
- [ ] 長時間稼働での安定性確認

### 互換性確認
- [ ] 既存のMCPクライアントが無変更で動作
- [ ] 既存の環境変数設定が継続動作
- [ ] Claude Desktopとの統合が正常
- [ ] カスタムクライアントとの統合が正常

### エラー処理確認
- [ ] 不正なリクエストで適切なエラー応答
- [ ] JSON-RPC 2.0形式違反の検出
- [ ] タイムアウト処理が正常動作
- [ ] 接続エラーの適切なハンドリング

MCPサーバーの詳細な設定と運用については、このガイドを参照してください。SDK移行に関する問題が解決しない場合は、ログファイルとともにサポートにお問い合わせください。
