# Pull Request

## Summary
<!-- Provide a brief description of what this PR accomplishes -->

## Type of Change
<!-- Mark the relevant option with an "x" -->
- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update
- [ ] Performance improvement
- [ ] Refactoring (no functional changes)

## Changes Made
<!-- Describe the specific changes implemented in this PR -->
-
-
-

## Motivation and Context
<!-- Why is this change required? What problem does it solve? -->
<!-- If it fixes an open issue, please link to the issue here -->
Fixes #(issue)

## How Has This Been Tested?
<!-- Describe the tests you ran to verify your changes -->
<!-- Provide instructions so reviewers can reproduce -->
- [ ] Unit tests (`go test ./...`)
- [ ] Integration tests
- [ ] Manual testing with local setup
- [ ] Tested with AWS services (S3 Vectors, OpenSearch, Bedrock)

### Test Configuration
- Go version:
- AWS Region:
- OpenSearch version (if applicable):

## Impact Analysis
<!-- What parts of the system does this change affect? -->
### Components Affected
- [ ] CLI commands (`cmd/`)
- [ ] Vectorization (`internal/vectorizer/`)
- [ ] OpenSearch integration (`internal/opensearch/`)
- [ ] S3 Vector operations (`internal/s3vector/`)
- [ ] Slack bot (`internal/slackbot/`)
- [ ] Bedrock embedding (`internal/embedding/`)
- [ ] Configuration (`internal/config/`)

### AWS Resources Impact
- [ ] No AWS resource changes
- [ ] S3 bucket operations
- [ ] OpenSearch index structure
- [ ] IAM permissions required
- [ ] Bedrock model usage

## Breaking Changes
<!-- List any breaking changes and migration steps if applicable -->
- [ ] None
- [ ] Yes (describe below)

### Migration Guide
<!-- If breaking changes, provide migration steps -->

## Dependencies
<!-- List any new dependencies added or updated -->
- [ ] No new dependencies
- [ ] Dependencies added/updated (list below)

## Documentation
<!-- Documentation changes required for this PR -->
- [ ] README.md updated
- [ ] CLAUDE.md updated
- [ ] Inline code comments added/updated
- [ ] API documentation updated
- [ ] Configuration examples updated

## Checklist
<!-- Mark completed items with an "x" -->
- [ ] My code follows the project's style guidelines (`go fmt ./...` and `go vet ./...`)
- [ ] I have performed a self-review of my own code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [ ] My changes generate no new warnings or errors
- [ ] I have added tests that prove my fix is effective or that my feature works
- [ ] New and existing unit tests pass locally with my changes
- [ ] Any dependent changes have been merged and published
- [ ] I have checked my code for any security issues or exposed secrets
- [ ] I have tested with the minimum supported Go version (1.23)
- [ ] I have run `go mod tidy` to clean up dependencies

## Performance Considerations
<!-- If applicable, describe any performance implications -->
- [ ] No performance impact
- [ ] Performance improved (describe metrics)
- [ ] Performance degraded but acceptable (explain trade-offs)

## Additional Notes
<!-- Any additional information that reviewers should know -->

## Screenshots/Logs
<!-- If applicable, add screenshots or logs to help explain your changes -->

---

# プルリクエスト（日本語版）

## 概要
<!-- このPRで達成される内容を簡潔に説明してください -->

## 変更の種類
<!-- 該当する項目に "x" を入れてマークしてください -->
- [ ] バグ修正（既存機能を破壊しない問題の修正）
- [ ] 新機能（既存機能を破壊しない機能の追加）
- [ ] 破壊的変更（既存機能の動作に影響を与える修正や機能）
- [ ] ドキュメント更新
- [ ] パフォーマンス改善
- [ ] リファクタリング（機能的変更なし）

## 実装された変更
<!-- このPRで実装された具体的な変更を記述してください -->
-
-
-

## 動機と背景
<!-- なぜこの変更が必要なのか？どのような問題を解決するのか？ -->
<!-- オープンなissueを修正する場合は、ここにissueをリンクしてください -->
Fixes #(issue)

## テスト方法
<!-- 変更を検証するために実行したテストを説明してください -->
<!-- レビュアーが再現できるよう手順を提供してください -->
- [ ] ユニットテスト（`go test ./...`）
- [ ] 統合テスト
- [ ] ローカル環境での手動テスト
- [ ] AWSサービス（S3 Vectors、OpenSearch、Bedrock）でのテスト

### テスト設定
- Goバージョン:
- AWSリージョン:
- OpenSearchバージョン（該当する場合）:

## 影響分析
<!-- この変更がシステムのどの部分に影響するか？ -->
### 影響を受けるコンポーネント
- [ ] CLIコマンド（`cmd/`）
- [ ] ベクトル化（`internal/vectorizer/`）
- [ ] OpenSearch統合（`internal/opensearch/`）
- [ ] S3 Vector操作（`internal/s3vector/`）
- [ ] Slack bot（`internal/slackbot/`）
- [ ] Bedrock埋め込み（`internal/embedding/`）
- [ ] 設定（`internal/config/`）

### AWSリソースへの影響
- [ ] AWSリソースの変更なし
- [ ] S3バケット操作
- [ ] OpenSearchインデックス構造
- [ ] IAM権限が必要
- [ ] Bedrockモデルの使用

## 破壊的変更
<!-- 破壊的変更がある場合は、移行手順と共にリストしてください -->
- [ ] なし
- [ ] あり（以下に記述）

### 移行ガイド
<!-- 破壊的変更がある場合は、移行手順を提供してください -->

## 依存関係
<!-- 追加または更新された新しい依存関係をリストしてください -->
- [ ] 新しい依存関係なし
- [ ] 依存関係の追加/更新（以下にリスト）

## ドキュメント
<!-- このPRに必要なドキュメント変更 -->
- [ ] README.md更新
- [ ] CLAUDE.md更新
- [ ] インラインコードコメントの追加/更新
- [ ] APIドキュメント更新
- [ ] 設定例の更新

## チェックリスト
<!-- 完了した項目に "x" をマークしてください -->
- [ ] コードがプロジェクトのスタイルガイドラインに従っている（`go fmt ./...` と `go vet ./...`）
- [ ] 自分のコードをセルフレビューした
- [ ] 理解が困難な領域にコメントを追加した
- [ ] ドキュメントに対応する変更を行った
- [ ] 変更によって新しい警告やエラーが生成されない
- [ ] 修正が効果的であることまたは機能が動作することを証明するテストを追加した
- [ ] 新しいテストと既存のユニットテストがローカルで成功する
- [ ] 依存する変更がマージされ公開されている
- [ ] セキュリティ問題や露出した秘密情報がないかコードをチェックした
- [ ] サポートされる最小Goバージョン（1.23）でテストした
- [ ] `go mod tidy`を実行して依存関係をクリーンアップした

## パフォーマンスに関する考慮事項
<!-- 該当する場合は、パフォーマンスへの影響を説明してください -->
- [ ] パフォーマンスへの影響なし
- [ ] パフォーマンス改善（メトリクスを記述）
- [ ] パフォーマンス低下だが許容範囲（トレードオフを説明）

## 追加ノート
<!-- レビュアーが知っておくべき追加情報 -->

## スクリーンショット/ログ
<!-- 該当する場合は、変更を説明するのに役立つスクリーンショットやログを追加してください -->