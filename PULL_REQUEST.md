# Pull Request

## Summary
- Exclude Slack bot-authored messages from hybrid Slack search results and metrics.

## Type of Change
- [x] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update
- [ ] Performance improvement
- [ ] Refactoring (no functional changes)

## Changes Made
- Filter Slack search messages by BotID/subtype/Slackbot/B-prefix/user fallback to drop bot posts.
- Record filtered bot count on spans and recompute match totals after filtering.
- Keep downstream context retrieval and response building limited to human-authored messages.

## Motivation and Context
Bot-authored Slack posts should not appear in hybrid search results; they are now removed before building responses.
Fixes #(issue)

## How Has This Been Tested?
- [x] Unit tests (`go test ./...`)
- [ ] Integration tests
- [ ] Manual testing with local setup
- [ ] Tested with AWS services (S3 Vectors, OpenSearch, Bedrock)

### Test Configuration
- Go version: (default)
- AWS Region: (n/a)
- OpenSearch version (if applicable): (n/a)

## Impact Analysis
### Components Affected
- [ ] CLI commands (`cmd/`)
- [ ] Vectorization (`internal/vectorizer/`)
- [ ] OpenSearch integration (`internal/opensearch/`)
- [ ] S3 Vector operations (`internal/s3vector/`)
- [x] Slack bot (`internal/slackbot/`)
- [ ] Bedrock embedding (`internal/embedding/`)
- [ ] Configuration (`internal/config/`)

### AWS Resources Impact
- [x] No AWS resource changes
- [ ] S3 bucket operations
- [ ] OpenSearch index structure
- [ ] IAM permissions required
- [ ] Bedrock model usage

## Breaking Changes
- [x] None
- [ ] Yes (describe below)

### Migration Guide

## Dependencies
- [x] No new dependencies
- [ ] Dependencies added/updated (list below)

## Documentation
- [ ] README.md updated
- [ ] CLAUDE.md updated
- [ ] Inline code comments added/updated
- [ ] API documentation updated
- [ ] Configuration examples updated

## Checklist
- [x] My code follows the project's style guidelines (`go fmt ./...` and `go vet ./...`)
- [x] I have performed a self-review of my own code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [x] My changes generate no new warnings or errors
- [ ] I have added tests that prove my fix is effective or that my feature works
- [x] New and existing unit tests pass locally with my changes
- [ ] Any dependent changes have been merged and published
- [x] I have checked my code for any security issues or exposed secrets
- [ ] I have tested with the minimum supported Go version (1.23)
- [ ] I have run `go mod tidy` to clean up dependencies

## Performance Considerations
- [x] No performance impact
- [ ] Performance improved (describe metrics)
- [ ] Performance degraded but acceptable (explain trade-offs)

## Additional Notes

## Screenshots/Logs

---

# プルリクエスト（日本語版）

## 概要
- Slack検索結果からBOT投稿を除外し、メトリクスも人間投稿のみで集計するようにしました。

## 変更の種類
- [x] バグ修正（既存機能を破壊しない問題の修正）
- [ ] 新機能（既存機能を破壊しない機能の追加）
- [ ] 破壊的変更（既存機能の動作に影響を与える修正や機能）
- [ ] ドキュメント更新
- [ ] パフォーマンス改善
- [ ] リファクタリング（機能的変更なし）

## 実装された変更
- BotID・subtype・Slackbot/Bプレフィックス・User欠落時のUsernameを基準にBOT投稿をフィルタ。
- フィルタ件数をスパン属性に記録し、マッチ件数を再計算してレスポンスを生成。
- 以降のコンテキスト取得/レスポンス構築はフィルタ後メッセージのみを使用。

## 動機と背景
Slackハイブリッド検索にBOT投稿を含めないようにするため。
Fixes #(issue)

## テスト方法
- [x] ユニットテスト（`go test ./...`）
- [ ] 統合テスト
- [ ] ローカル環境での手動テスト
- [ ] AWSサービス（S3 Vectors、OpenSearch、Bedrock）でのテスト

### テスト設定
- Goバージョン: (default)
- AWSリージョン: (n/a)
- OpenSearchバージョン（該当する場合）: (n/a)

## 影響分析
### 影響を受けるコンポーネント
- [ ] CLIコマンド（`cmd/`）
- [ ] ベクトル化（`internal/vectorizer/`）
- [ ] OpenSearch統合（`internal/opensearch/`）
- [ ] S3 Vector操作（`internal/s3vector/`）
- [x] Slack bot（`internal/slackbot/`）
- [ ] Bedrock埋め込み（`internal/embedding/`）
- [ ] 設定（`internal/config/`）

### AWSリソースへの影響
- [x] AWSリソースの変更なし
- [ ] S3バケット操作
- [ ] OpenSearchインデックス構造
- [ ] IAM権限が必要
- [ ] Bedrockモデルの使用

## 破壊的変更
- [x] なし
- [ ] あり（以下に記述）

### 移行ガイド

## 依存関係
- [x] 新しい依存関係なし
- [ ] 依存関係の追加/更新（以下にリスト）

## ドキュメント
- [ ] README.md更新
- [ ] CLAUDE.md更新
- [ ] インラインコードコメントの追加/更新
- [ ] APIドキュメント更新
- [ ] 設定例の更新

## チェックリスト
- [x] コードがプロジェクトのスタイルガイドラインに従っている（`go fmt ./...` と `go vet ./...`）
- [x] 自分のコードをセルフレビューした
- [ ] 理解が困難な領域にコメントを追加した
- [ ] ドキュメントに対応する変更を行った
- [x] 変更によって新しい警告やエラーが生成されない
- [ ] 修正が効果的であることまたは機能が動作することを証明するテストを追加した
- [x] 新しいテストと既存のユニットテストがローカルで成功する
- [ ] 依存する変更がマージされ公開されている
- [x] セキュリティ問題や露出した秘密情報がないかコードをチェックした
- [ ] サポートされる最小Goバージョン（1.23）でテストした
- [ ] `go mod tidy`を実行して依存関係をクリーンアップした

## パフォーマンスに関する考慮事項
- [x] パフォーマンスへの影響なし
- [ ] パフォーマンス改善（メトリクスを記述）
- [ ] パフォーマンス低下だが許容範囲（トレードオフを説明）

## 追加ノート

## スクリーンショット/ログ
