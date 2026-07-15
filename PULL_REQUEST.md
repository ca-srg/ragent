# Pull Request

## Summary

When a Slack permalink in the MCP `hybrid_search` query resolves successfully, RAGent now short-circuits document hybrid search and returns the fetched message (including thread replies) in `referenced_slack_urls` first. This prevents MCP clients from answering from unrelated knowledge-base hits when the user clearly referenced a Slack conversation.

## Type of Change
- [ ] Bug fix (non-breaking change which fixes an issue)
- [x] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update
- [x] Performance improvement
- [ ] Refactoring (no functional changes)

## Changes Made
- Early-return path when Slack URL fetch yields messages: `search_method=slack_url_direct_fetch`, empty `results`, sources `["slack_urls"]`
- Include `thread_replies` on `HybridSearchSlackResult` (and OpenAPI) so thread context is available to MCP clients
- Reorder `HybridSearchResponse` JSON so `referenced_slack_urls` appears before `results`
- Update hybrid_search tool descriptions (JA/EN) to instruct clients to prioritize `referenced_slack_urls`
- Extend unit tests to cover direct-fetch response shape, thread replies, and no OpenSearch calls

## Motivation and Context

Previously, even after a Slack permalink was fetched, the tool continued hybrid document search and returned both document hits and the referenced Slack message. LLM clients often over-weighted document `results` and underused the explicitly referenced Slack conversation. Operators asking “この URL の内容を教えて” expect the linked message (and thread) to be the primary answer source.

This change makes successful Slack URL resolution the authoritative path: skip BM25/vector search, avoid unnecessary OpenSearch/Bedrock cost, and surface thread replies so answers can include the full conversation context.

## How Has This Been Tested?
- [x] Unit tests (`go test ./internal/mcpserver/`)
- [ ] Integration tests
- [ ] Manual testing with local setup
- [ ] Tested with AWS services (S3 Vectors, OpenSearch, Bedrock)

### Test Configuration
- Go version: local toolchain (module Go 1.23+)
- AWS Region: N/A (unit tests only)
- OpenSearch version (if applicable): N/A

### Commands and Results
```text
gofmt -l <changed go files>          # no output (clean)
go vet ./internal/mcpserver/         # pass
go test -v -count=1 ./internal/mcpserver/ -timeout 120s
  # PASS (includes TestHybridSearchToolFetchesReferencedSlackURLWithoutSlackSearchService
  #       and related Slack URL fetch tests)
```

## Impact Analysis
### Components Affected
- [ ] CLI commands (`cmd/`)
- [ ] Vectorization (`internal/vectorizer/`)
- [ ] OpenSearch integration (`internal/opensearch/`)
- [ ] S3 Vector operations (`internal/s3vector/`)
- [ ] Slack bot (`internal/slackbot/`)
- [ ] Bedrock embedding (`internal/embedding/`)
- [ ] Configuration (`internal/config/`)
- [x] MCP server (`internal/mcpserver/`) — hybrid_search tool behavior and response schema
- [x] OpenAPI (`openapi.yaml`) — Slack result schema

### AWS Resources Impact
- [x] No AWS resource changes
- [ ] S3 bucket operations
- [ ] OpenSearch index structure
- [ ] IAM permissions required
- [ ] Bedrock model usage

## Breaking Changes
- [ ] None
- [x] Yes (describe below)

### Migration Guide
Behavioral change for MCP `hybrid_search` when a Slack permalink resolves:
1. Document hybrid search is **skipped** (no OpenSearch/BM25 or vector query).
2. Response uses `search_method: "slack_url_direct_fetch"`, `results: []`, and populated `referenced_slack_urls`.
3. `HybridSearchSlackResult` may include optional `thread_replies`.

Clients that assumed hybrid document results always accompany URL fetch should read `referenced_slack_urls` first (tool description now states this). Failed URL fetch still falls through to normal hybrid search.

## Dependencies
- [x] No new dependencies
- [ ] Dependencies added/updated (list below)

## Documentation
- [ ] README.md updated
- [ ] CLAUDE.md updated
- [ ] Inline code comments added/updated
- [x] API documentation updated (`openapi.yaml`)
- [ ] Configuration examples updated

## Checklist
- [x] My code follows the project's style guidelines (`go fmt` / `gofmt` and `go vet`)
- [x] I have performed a self-review of my own code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [x] I have made corresponding changes to the documentation
- [x] My changes generate no new warnings or errors
- [x] I have added tests that prove my fix is effective or that my feature works
- [x] New and existing unit tests pass locally with my changes
- [ ] Any dependent changes have been merged and published
- [x] I have checked my code for any security issues or exposed secrets
- [ ] I have tested with the minimum supported Go version (1.23)
- [ ] I have run `go mod tidy` to clean up dependencies (no module changes)

## Performance Considerations
- [ ] No performance impact
- [x] Performance improved (describe metrics)
- [ ] Performance degraded but acceptable (explain trade-offs)

Successful Slack URL resolution skips OpenSearch health check, embedding, BM25, and vector search for that request.

## File-by-File Changes

| File | Change | Purpose |
| --- | --- | --- |
| `internal/mcpserver/hybrid_search_tool.go` | Add direct-fetch early return, `convertSlackURLDirectResponse`, thread reply mapping, tool description update | Prioritize resolved Slack URLs and avoid document search noise/cost |
| `internal/mcpserver/types.go` | Move `referenced_slack_urls` before `results`; add `ThreadReplies` on `HybridSearchSlackResult` | JSON field order for client priority; expose thread context |
| `internal/mcpserver/command.go` | Update hybrid_search tool description (JA/EN) | Instruct MCP clients to prioritize `referenced_slack_urls` |
| `internal/mcpserver/hybrid_search_tool_slack_url_test.go` | End-to-end HandleToolCall assertions for direct fetch, threads, no OS calls | Guard the new behavior |
| `openapi.yaml` | Document `thread_replies` on Slack result schema | Keep API schema aligned |
| `PULL_REQUEST.md` | PR description for review | Reviewer context |

## Additional Notes

- Related prior work: Slack URL fetch without full Slack search (#67).
- No linked GitHub Issue for this change.
- `PULL_REQUEST.md` is committed only to satisfy PR workflow requirements; it may be ignored after merge.

## Screenshots/Logs
N/A

---

# プルリクエスト（日本語版）

## 概要

MCP `hybrid_search` でクエリ内の Slack permalink が取得できた場合、文書ハイブリッド検索を行わず `referenced_slack_urls`（スレッド返信含む）を最優先で返すようにした。ユーザーが明示的に参照した会話を、無関係なドキュメントヒットより優先させる。

## 変更の種類
- [ ] バグ修正（既存機能を破壊しない問題の修正）
- [x] 新機能（既存機能を破壊しない機能の追加）
- [ ] 破壊的変更（既存機能の動作に影響を与える修正や機能）
- [ ] ドキュメント更新
- [x] パフォーマンス改善
- [ ] リファクタリング（機能的変更なし）

## 実装された変更
- Slack URL 取得成功時の早期 return（`search_method=slack_url_direct_fetch`）
- `thread_replies` を hybrid_search の Slack 結果に追加
- レスポンス JSON で `referenced_slack_urls` を `results` より前に配置
- ツール説明文を更新し、クライアントへ優先利用を指示
- ユニットテストで直接取得パス・スレッド・OpenSearch 非呼び出しを検証

## 動機と背景

Slack URL を取得できても従来は文書検索も並行して走り、LLM がドキュメント結果を優先してしまうことがあった。「この URL の内容を教えて」系の問い合わせでは、参照メッセージとスレッドを一次情報にすべき。関連 Issue なし。

## 変更される挙動

| 条件 | 変更前 | 変更後 |
| --- | --- | --- |
| Slack permalink 取得成功 | 文書 hybrid 検索も実行し、両方を返す | 文書検索をスキップし `referenced_slack_urls` のみ返却 |
| 取得失敗 / URL なし | 従来どおり hybrid 検索 | 変更なし |
| Slack 結果のスレッド | hybrid_search 側では返信が欠落しがち | `thread_replies` に返信を含める |

## テスト方法
- [x] ユニットテスト（`go test -v -count=1 ./internal/mcpserver/ -timeout 120s` → PASS）
- [x] `gofmt` / `go vet ./internal/mcpserver/` → PASS
- [ ] 統合テスト
- [ ] ローカル環境での手動テスト
- [ ] AWSサービスでのテスト

### テスト設定
- Goバージョン: ローカル toolchain（module 1.23+）
- AWSリージョン: N/A
- OpenSearchバージョン: N/A

## 影響分析
### 影響を受けるコンポーネント
- [x] MCP server（`internal/mcpserver/`）
- [x] OpenAPI（`openapi.yaml`）

### AWSリソースへの影響
- [x] AWSリソースの変更なし

## 破壊的変更
- [ ] なし
- [x] あり（以下に記述）

Slack permalink 取得成功時は文書検索結果が空になる。`referenced_slack_urls` を読むクライアント実装が必要（ツール説明でも指示済み）。取得失敗時は従来パスにフォールバック。

## 依存関係
- [x] 新しい依存関係なし

## ドキュメント
- [x] APIドキュメント更新（`openapi.yaml`）

## チェックリスト
- [x] コードがプロジェクトのスタイルガイドラインに従っている（`gofmt` と `go vet`）
- [x] 自分のコードをセルフレビューした
- [x] 変更によって新しい警告やエラーが生成されない
- [x] 修正が効果的であることまたは機能が動作することを証明するテストを追加した
- [x] 新しいテストと既存のユニットテストがローカルで成功する
- [x] セキュリティ問題や露出した秘密情報がないかコードをチェックした

## パフォーマンスに関する考慮事項
- [x] パフォーマンス改善: Slack URL 直接取得成功時は OpenSearch / 埋め込みを実行しない

## 懸念

- URL が解決できた場合に文書コンテキストが一切付かないため、「URL + 関連ドキュメントも欲しい」ケースでは不足しうる。必要なら後続で opt-in フラグ検討。
- JSON フィールド順は Go の struct タグ順に依存。クライアントがフィールド順に依存しない実装なら問題なし（テストで順序も確認済み）。

## 追加ノート
- 先行変更: Slack URL fetch without full Slack search (#67)
