# Secret ドキュメントのアクセス制御

## 概要

RAGent では、ドキュメントに `secret: true` メタデータを付与することで機密文書として扱えます。機密文書は認可されたユーザーのみが検索結果で閲覧でき、それ以外のユーザーには検索結果から自動的に除外されます。

設計方針は **フェイルクローズ**（デフォルト拒否）です。明示的に許可されない限り、機密文書にはアクセスできません。

## アーキテクチャ

```
┌──────────────┐     ┌─────────────────────┐     ┌────────────────────┐
│   Ingestion  │     │   Access Control     │     │    OpenSearch       │
│              │     │                      │     │                    │
│  secret:true ├────►│  ExcludeSecret=true? ├────►│  must_not:          │
│  メタデータ   │     │  チャネル別判定       │     │   term:secret:true  │
└──────────────┘     └─────────────────────┘     └────────────────────┘
```

### データフロー

1. **取り込み時**: ドキュメントの Front Matter または WebUI アップロード時に `secret: true` を設定
2. **格納時**: OpenSearch インデックスおよび S3 Vectors に `secret` フィールドとして保存
3. **検索時**: チャネル（Slack Bot / MCP Server / CLI）ごとにアクセス判定を行い、`ExcludeSecret` フラグを OpenSearch クエリに反映

## チャネル別アクセス制御

| チャネル | 制御方法 | デフォルト |
|---|---|---|
| Slack Bot | `slack-secret.yaml` 許可リスト | 拒否（許可リスト未設定時） |
| MCP Server | OIDC 認証の有無 | 拒否（IP 認証のみの場合） |
| CLI（query / chat） | ハードコードで除外 | 常に拒否 |

### Slack Bot

Slack Bot では `slack-secret.yaml` ファイルで許可するユーザーを管理します。

#### 設定ファイル

デフォルトパス: `~/.config/ragent/slack-secret.yaml`

```yaml
allowed_users:
  - U0123456789   # Slack ユーザーID
  - U9876543210
```

プロジェクトルートに `slack-secret.yaml.example` を用意しています。コピーして利用してください。

```bash
cp slack-secret.yaml.example ~/.config/ragent/slack-secret.yaml
```

#### Slack ユーザー ID の確認方法

1. Slack で対象ユーザーのプロフィールを開く
2. 「...」（その他） → 「メンバー ID をコピー」
3. `U` で始まる ID（例: `U0123456789`）を許可リストに追加

#### 判定フロー

```
shouldExcludeSecret(opts)
  │
  ├─ slack-secret.yaml を読み込む
  │   ├─ ファイルが存在しない → 全員拒否
  │   └─ YAML が不正 → 全員拒否（フェイルクローズ）
  │
  └─ SecretAccessChecker.CanAccessSecret(false, slackUserID)
      ├─ 許可リストに含まれる → ExcludeSecret=false（アクセス可）
      └─ 含まれない → ExcludeSecret=true（アクセス不可）
```

### MCP Server

MCP Server では OIDC 認証の有無でアクセスが決まります。IP 認証のみのユーザーは機密文書にアクセスできません。

| 認証方法 | secret アクセス | 説明 |
|---|---|---|
| `--auth-method oidc` | ✅ 許可 | OIDC 認証成功時 |
| `--auth-method ip` | ❌ 拒否 | IP 認証のみでは不可 |
| `--auth-method either`（OIDC 成功） | ✅ 許可 | OIDC が優先評価される |
| `--auth-method either`（IP フォールバック） | ❌ 拒否 | OIDC 未認証時 |
| `--auth-method both` | ✅ 許可 | OIDC 認証を含むため |

#### 判定フロー

```
applySecretPolicyFromContext(ctx, request)
  │
  ├─ デフォルト: ExcludeSecret=true（拒否）
  │
  └─ context に OIDC トークンが存在するか？
      ├─ 存在する → ExcludeSecret=false（許可）
      └─ 存在しない → ExcludeSecret=true のまま（拒否）
```

#### フィルタバイパスの防止

クライアントが `filters.secret` パラメータを送信しても、サーバー側で自動的に除去されます。ユーザーがリクエストパラメータを操作してポリシーを回避することはできません。

```go
// hybrid_search_tool.go
if strings.EqualFold(k, "secret") {
    continue // secret フィルタは無視される
}
```

### CLI（query / chat）

CLI コマンドでは `ExcludeSecret: true` がハードコードされており、機密文書には一切アクセスできません。ユーザーが `--filter` で `secret` を指定した場合も自動的に除去されます。

```go
// query_command.go / chat_command.go
ExcludeSecret: true  // 常に除外
```

```go
// query_command.go — フィルタから secret キーを除去
if strings.EqualFold(key, "secret") {
    continue
}
```

## ドキュメントに secret を設定する方法

### Front Matter（Markdown）

```markdown
---
title: 機密設計書
category: architecture
secret: true
---

# 内部設計

この文書は機密です。
```

### WebUI アップロード

WebUI のアップロードフォームで `secret` フラグを `true` に設定できます。

### OpenSearch インデックスへの直接反映

取り込み時に `secret` フィールドが OpenSearch ドキュメントに `boolean` 型で格納されます。

```json
{
  "content": "機密情報...",
  "title": "機密設計書",
  "secret": true,
  "embedding": [0.1, 0.2, ...]
}
```

## OpenSearch クエリでの除外メカニズム

`ExcludeSecret=true` の場合、BM25・ベクトル検索・TermQuery の全てに以下の条件が追加されます:

```json
{
  "bool": {
    "must_not": [
      { "term": { "secret": true } }
    ]
  }
}
```

これにより `secret: true` のドキュメントが検索結果から完全に除外されます。

## セキュリティ上の考慮事項

- **フェイルクローズ**: 設定ファイルの欠落・不正はすべて「拒否」として扱われます
- **サーバー側ポリシー強制**: クライアントからの `filters.secret` パラメータは無視されます
- **OIDC 優先評価**: `either` モードでは OIDC が先に評価され、成功すれば secret アクセスが有効になります
- **Slack ユーザー ID の重複排除**: 許可リストは読み込み時に正規化・重複排除されます
