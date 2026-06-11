# RAGent における Slack User Token 必要性の根拠

> **対象読者**: Slack ワークスペース管理者
> **目的**: RAGent の Slack 検索機能に対して User Token (`xoxp-`) の発行・スコープ付与の判断材料を提供する

---

## TL;DR

- RAGent は Slack 検索バックエンドを 2 つ持つ:
  - **`search.messages` (User Token / `xoxp-`)** — 従来からの全文検索 API。private channel / DM / MPIM もユーザーの権限範囲で検索可能。
  - **`assistant.search.context` (Bot Token / `xoxb-` + `action_token`)** — Slack の Real-time Search API。Bot 未参加 public channel まで検索可能だが、Slack イベント (`app_mention` / DM `message`) から取得する短命の `action_token` が無いと呼べない。
- **slack-bot のメンション応答** は Bot Token + `action_token` で `assistant.search.context` を使えるため、User Token 不要でも動作する (ただし public channel 限定)。
- **`query` CLI / `mcp-server`** は Slack イベント駆動ではないため `action_token` を取得できず、結果として `search.messages` (User Token) しか選べない。**User Token が無いとこれらの経路では Slack 検索が無効化される**。
- 全経路で Slack 検索を有効化したい場合、および public 以外 (private / DM / MPIM) も検索対象に含めたい場合は **`search:read` を持つ User Token が必須**。

---

## 1. 背景: RAGent が Slack 検索で行うこと

RAGent は社内ナレッジ検索を行う RAG (Retrieval-Augmented Generation) システムで、以下の経路で Slack 検索を使う。経路ごとに使える API と権限が異なる。

| 利用シーン | User Token 設定時 | User Token 未設定時 |
|---|---|---|
| `slack-bot` (Bot へのメンション応答) | `search.messages` (public + private + DM + MPIM) | `assistant.search.context` (public のみ。Bot 未参加 ch も検索可) |
| `query` (CLI) | `search.messages` (public + private + DM + MPIM) | **Slack 検索 skip** (action_token を取れないため) |
| `mcp-server` (外部 AI クライアントへの MCP ツール提供) | `search.messages` (public + private + DM + MPIM) | **Slack 検索 skip** (同上) |

つまり、User Token がなくても slack-bot のメンション応答経路だけは部分的に動作するが、`query` / `mcp-server` で Slack 検索を使いたい場合や、private channel / DM / MPIM を検索対象に含めたい場合は User Token が必須となる。

---

## 2. 技術的根拠: Slack API の制約

### 2.1 `search.messages` は User Token 専用

Slack 公式 docs ([search.messages](https://docs.slack.dev/reference/methods/search.messages)) に明記:

| 項目 | 値 |
|---|---|
| Required scope | **User token scope: `search:read`** |
| Bot token support | **なし** |
| ステータス | **Legacy method** (新規開発は `assistant.search.context` 推奨) |

`search.all`, `search.files` も同様に User Token 専用。

### 2.2 Bot 用 `search:read.public` は別 API のためのスコープ

Slack には `search:read.public` / `search:read.private` / `search:read.im` / `search:read.mpim` / `search:read.users` / `search:read.files` という granular scope 群があるが、これらは **Real-time Search API (`assistant.search.context`)** のためのスコープであり、`search.messages` では使えない。

→ 公式 docs: <https://docs.slack.dev/reference/scopes/search.read.public>

### 2.3 実機検証結果

ワークスペース `ca-winticket.slack.com` で発行された Bot Token (xoxb) に `search:read.public` 等のスコープを付与した状態で `search.messages` を呼んだところ、以下のレスポンス:

```json
{ "ok": false, "error": "not_allowed_token_type" }
```

Slack 側が **token type そのもので拒否** しており、スコープを追加しても解消しない (公式仕様と一致)。

---

## 3. 代替手段が成立しない理由

### 3.1 `assistant.search.context` (Real-time Search API) — slack-bot だけ部分的に成立

公式 docs: <https://docs.slack.dev/reference/methods/assistant.search.context>

- Bot Token でも呼び出し可能だが、**呼び出し時に `action_token` (Slack イベントに同梱される短命トークン) が必須**
- `action_token` は `app_mention` / `message.im` などの **Slack イベント受信時のみ取得可能** (`event.assistant_thread.action_token`)
- 検索範囲は **public channel のみ**。`search:read.private` / `search:read.im` / `search:read.mpim` は **User Token only** スコープなので、Bot Token 経由では private/DM/MPIM を検索できない
- 検索は `on behalf of the authenticated user` で実行されるので、メンションしたユーザーがアクセスできない情報は返らない (情報漏えい防止)
- **RAGent では slack-bot のメンション応答経路だけが action_token を取れる**ため、この経路に限り User Token なしでも検索可能 (public ch 限定)
- `query` / `mcp-server` は Slack イベント駆動ではないため `action_token` を取得する手段がなく、この API を使えない

→ **slack-bot のメンション応答時のみ部分的に代替可能**だが、`query` / `mcp-server` の検索経路は救えない。また private / DM / MPIM はどの経路でも User Token なしでは検索できない。

### 3.2 `conversations.history` でクライアントサイド全文検索

- Bot Token + `channels:history` 等で取得可能だが、**全文検索ではない** (チャネル単位での履歴取得)
- 検索ワードでのフィルタリングはクライアントサイド実装が必要
- チャネルを事前に列挙する必要があり、ワークスペース全体を横断できない
- メッセージ件数次第で API レート制限・遅延が深刻 (数万件以上のチャネルでは現実的でない)
- スレッド・添付ファイル・メンション展開の取り扱いを自前実装する必要がある

→ RAGent のように「自然文クエリで関連会話を横断検索」するユースケースには**機能的にも性能的にも不適**。

### 3.3 Enterprise Grid 限定 API

`admin.conversations.search` 等は Enterprise Grid 限定かつ管理者用途で、一般のメッセージ全文検索ではない。

---

## 4. 申請してほしい User Token と最小スコープ

### 4.1 User Token (`xoxp-`)

検索したい範囲に応じて以下を付与:

| スコープ | 必須度 | 用途 |
|---|---|---|
| `search:read` | **必須** | `search.messages` 本体 |
| `channels:history` | 強く推奨 | 検索でヒットした public channel のメッセージ・スレッド本文を取得 |
| `groups:history` | 推奨 | 検索でヒットした private channel のメッセージ・スレッド本文を取得 |
| `im:history` | 任意 | DM を検索対象に含める場合 |
| `mpim:history` | 任意 | group DM を検索対象に含める場合 |
| `users:read` | 推奨 | メッセージ送信者の表示名を取得 |
| `channels:read` / `groups:read` | 推奨 | チャネル名解決 |

### 4.2 Bot Token (`xoxb-`) (既存)

RAGent の応答送信や URL 直接取得で別途必要 (本資料の主題ではないので割愛)。

---

## 5. セキュリティ上の留意点

| 項目 | 対応 |
|---|---|
| トークン発行ユーザー | **専用のシステムユーザー (例: `rag-bot`) で発行する**。個人ユーザーのトークンは退職等で失効するため避ける |
| 保管場所 | **AWS Secrets Manager** に格納し、RAGent は `SECRET_MANAGER_SECRET_ID` 経由で起動時に注入する (.env 直書き禁止) |
| ローテーション | Slack OAuth で Token Rotation を有効化、もしくは定期手動ローテーションを運用に組み込む |
| アクセス可能範囲 | User Token の権限は発行ユーザーが参加しているチャネルに準ずる。検索対象を絞りたい場合は発行ユーザーの所属チャネルで制御する |
| 監査 | Slack の Audit Logs (Enterprise Grid) で API 呼び出しを監視可能 |

---

## 6. `SLACK_USER_TOKEN` 不要で slack-bot を動かす場合の Slack App 設定

User Token を発行せず、slack-bot 経路だけ `assistant.search.context` (Real-time Search API) で動かす場合、Slack App を **AI-enabled app (Agents & AI Apps)** として構成する必要があります。これを満たさないと、`app_mention` イベント payload に `action_token` が一切含まれません (RAGent のログでは `action_token_probe top_level=false assistant_thread=false` として観測されます)。

公式の根拠:

> The `app_mention` event contains an `action_token` in the payload when the app is mentioned using `@app-name`.
> — https://docs.slack.dev/apis/web-api/real-time-search-api/#action-token

ただし RTS 自体は AI-enabled app 向けです。通常の bot のままでは token は出ません。

### 6.1 必要な Slack App 設定

1. **App Settings を開く**: <https://api.slack.com/apps>{your_app_id}
2. **「Agents & AI Apps」を有効化** (左サイドバー → Agents & AI Apps → Enable)
   - これで `assistant:write` scope が自動的に bot に追加されます (公式: <https://docs.slack.dev/ai/developing-agents#enabling-the-assistant-feature>)
3. **Manifest に `features.assistant_view` を追加** (UI 設定で「Agents & AI Apps」を有効化すると裏側で同等のものが入りますが、manifest 管理しているなら明示):
   ```yaml
   features:
     assistant_view:
       assistant_description: "RAGent answers questions using Slack context."
       suggested_prompts: []
   ```
4. **Bot scopes に以下を含める** (どれも既存運用と非破壊):
   - `app_mentions:read` (既存)
   - `chat:write` (既存)
   - `assistant:write` (Agents & AI Apps を ON にすれば自動付与)
   - `search:read.public` (Real-time Search API 用)
   - `im:history` (DM 経由のメンションを扱うなら)
5. **Event Subscriptions に以下を含める**:
   - `app_mention` (既存)
   - `message.im` (DM 検索を許すなら)
   - `assistant_thread_started`
   - `assistant_thread_context_changed`
   - 公式: <https://docs.slack.dev/ai/developing-agents#enabling-the-assistant-feature>
6. **Workspace に Reinstall** (Manage Distribution → Reinstall)
   - **必須**。Scope や Manifest を変更しても再インストールしない限り発行済みトークンには反映されません。

### 6.2 App 種別の制約

公式 docs:

> The RTS API is available for directory-published apps and internal apps only.
> — https://docs.slack.dev/apis/web-api/real-time-search-api/#overview

- **Internal app** (Workspace 専用 / 単一 Workspace install): ✅ 利用可
- **Slack Marketplace (directory-published)**: ✅ 利用可
- **Unlisted distributed app** (公開せず distributed としているもの): ❌ 利用不可

社内利用なら "Internal app" として作っておけば OK です。

### 6.3 Guests は対象外

> Workspace guests are not permitted to access apps using platform AI features...
> — https://docs.slack.dev/apis/web-api/real-time-search-api/#guests

ゲストユーザーが `@bot` を呼んでも token は発行されません。

### 6.4 設定後の確認方法

RAGent は受信した event の raw payload を probe して以下のログを出します:

```
event=events_api action_token_probe top_level=<bool> assistant_thread=<bool>
```

| パターン | 状況 | 次のアクション |
|---|---|---|
| `top_level=false assistant_thread=false` | Slack が token を発行していない | Agents & AI Apps 有効化 / Reinstall を確認 |
| `top_level=true` または `assistant_thread=true` | token は受け取れている | `assistant.search.context` が呼ばれるはず |

`has_action_token=true` がログに出れば、`assistant.search.context` への切り替えは自動で行われます。

---

## 7. User Token を発行できない場合の影響

User Token を付与できない場合、RAGent は以下の動作になる:

- `slack-bot` のメンション応答経路 — `assistant.search.context` (Bot Token + `action_token`) で **public channel の検索のみ動作**。Bot 未参加の public channel も検索ヒット可能。**private channel / DM / MPIM は検索不可**
- `query` CLI / `mcp-server` — Slack 検索は **無効化**される (`action_token` を取得できないため `assistant.search.context` を呼べず、User Token もないため `search.messages` も呼べない)。明示的にエラーメッセージを返すか、警告ログを出して Slack 検索をスキップして RAGent 本体は動作する
- OpenSearch を使った社内ドキュメント検索 (Markdown / GitHub 等) は **引き続き利用可能**
- `slack-bot` 自体はメンション応答や URL 引用などの基本機能は動作する (Bot Token のみで OK)

検索品質と網羅性を考えると User Token を発行することが推奨されるが、最小構成で運用したい場合は **slack-bot 経路 + public ch 検索のみ** で運用することも可能。

---

## 8. 参考リンク (Slack 公式)

- [`search.messages` — Legacy User Token 専用](https://docs.slack.dev/reference/methods/search.messages)
- [`search:read` (legacy user scope)](https://docs.slack.dev/reference/scopes/search.read)
- [`search:read.public` (Bot 用 granular scope。`assistant.search.context` 用)](https://docs.slack.dev/reference/scopes/search.read.public)
- [`assistant.search.context` (Real-time Search API)](https://docs.slack.dev/reference/methods/assistant.search.context)
- [Real-time Search API ガイド](https://docs.slack.dev/apis/web-api/real-time-search-api)
- [Changelog: Slack MCP server / Real-time Search API (2026-02-17)](https://docs.slack.dev/changelog/2026/02/17/slack-mcp)
- [Agents & AI Apps を有効化する手順](https://docs.slack.dev/ai/developing-agents#enabling-the-assistant-feature)
- [`assistant_thread_started` event](https://docs.slack.dev/reference/events/assistant_thread_started)
- [App Manifest reference](https://docs.slack.dev/reference/app-manifest/)

---

## 付録: 検証コマンド (再現用)

```bash
# 1. Bot Token の有効性とスコープを確認
curl -s -X POST "https://slack.com/api/auth.test" \
  -H "Authorization: Bearer xoxb-***" \
  -D headers.txt | jq .
grep -i "x-oauth-scopes" headers.txt

# 2. search.messages を Bot Token で叩く (失敗する)
curl -s -X POST "https://slack.com/api/search.messages" \
  -H "Authorization: Bearer xoxb-***" \
  --data-urlencode "query=hello" | jq .
# => {"ok": false, "error": "not_allowed_token_type"}

# 3. search.messages を User Token で叩く (成功する)
curl -s -X POST "https://slack.com/api/search.messages" \
  -H "Authorization: Bearer xoxp-***" \
  --data-urlencode "query=hello" | jq .
# => {"ok": true, "messages": {...}}

# 4. assistant.search.context は action_token が必要 (action_token 単独では
#    手動再現できないので、Bot の app_mention イベント payload から取り出した
#    値を使う)。RAGent では slack-bot 経路で自動的に処理される。
curl -s -X POST "https://slack.com/api/assistant.search.context" \
  -H "Authorization: Bearer xoxb-***" \
  -H "Content-Type: application/json; charset=utf-8" \
  --data '{"query":"hello","action_token":"<from app_mention event>"}' | jq .
```
