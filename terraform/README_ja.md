# RAGent Terraform モジュール

RAGent を AWS 上にデプロイするための Terraform モジュールです。EC2 または Fargate をコンピュートに、S3 Vectors または sqlite-vec をベクトル DB に、AWS OpenSearch または Docker OpenSearch を検索エンジンとして選択できます。

## 目次

- [ディレクトリ構成](#ディレクトリ構成)
- [デプロイパターン](#デプロイパターン)
- [前提条件](#前提条件)
- [クイックスタート](#クイックスタート)
- [利用例](#利用例)
- [モジュール入力変数](#モジュール入力変数)
- [モジュール出力](#モジュール出力)
- [作成される AWS リソース](#作成される-aws-リソース)
- [セキュリティグループ](#セキュリティグループ)
- [IAM ポリシー](#iam-ポリシー)
- [テンプレート（EC2 モード）](#テンプレートec2-モード)
- [テスト](#テスト)
- [Secrets Manager シークレット登録ガイド](#secrets-manager-シークレット登録ガイド)

## ディレクトリ構成

```
terraform/
├── README_ja.md                          # 本ドキュメント
├── secret-template.json                  # Secrets Manager 用テンプレート
├── aws/
│   ├── modules/
│   │   └── ragent/                       # メインモジュール
│   │       ├── main.tf                   #   エントリポイント（モード組み合わせバリデーション）
│   │       ├── locals.tf                 #   ローカル変数（条件分岐、環境変数マップ）
│   │       ├── variables.tf              #   入力変数（30+ 項目、バリデーション付き）
│   │       ├── outputs.tf                #   出力（15 項目）
│   │       ├── versions.tf               #   Terraform >= 1.5, AWS provider >= 5.98
│   │       ├── alb.tf                    #   ALB, ターゲットグループ, リスナー
│   │       ├── ec2.tf                    #   EC2 インスタンス, AMI, user-data
│   │       ├── fargate.tf                #   ECS クラスタ, タスク定義, サービス
│   │       ├── iam.tf                    #   IAM ロール・ポリシー
│   │       ├── opensearch.tf             #   AWS OpenSearch ドメイン
│   │       ├── s3vectors.tf              #   S3 Vectors バケット・インデックス
│   │       ├── secretsmanager.tf         #   Secrets Manager シークレット
│   │       ├── security_groups.tf        #   セキュリティグループ
│   │       ├── templates/                #   EC2 cloud-init / systemd テンプレート
│   │       └── tests/                    #   terraform test ファイル
│   └── examples/                         # 利用例
│       ├── ragent-ec2-complete/          #   EC2 フル機能構成
│       ├── ragent-ec2-s3vectors/         #   EC2 + S3 Vectors + AWS OpenSearch
│       ├── ragent-ec2-sqlite/            #   EC2 + sqlite + Docker OpenSearch
│       └── ragent-fargate-s3vectors/     #   Fargate + S3 Vectors + AWS OpenSearch
```

## デプロイパターン

3 つの有効なデプロイパターンがあります。

| パターン | `compute_type` | `vector_backend` | `opensearch_mode` | 用途 |
|---|---|---|---|---|
| **EC2 + S3 Vectors + AWS OpenSearch** | `ec2` | `s3` | `aws` | 本番環境（マネージドサービス活用） |
| **EC2 + sqlite + Docker OpenSearch** | `ec2` | `sqlite` | `docker` | 開発環境・PoC（外部依存なし） |
| **Fargate + S3 Vectors + AWS OpenSearch** | `fargate` | `s3` | `aws` | サーバーレスコンテナ本番環境 |

### 無効な組み合わせ

以下の組み合わせはモジュールの precondition で拒否されます：

| 組み合わせ | 理由 |
|---|---|
| Fargate + sqlite | EFS 未対応のため永続化不可 |
| Fargate + Docker OpenSearch | Fargate 内で Docker 実行不可 |
| Fargate で `container_image_uri` 未指定 | コンテナイメージが必須 |

## 前提条件

- Terraform >= 1.5
- AWS Provider >= 5.98
- VPC とサブネット（プライベートサブネット推奨）が作成済み
- Fargate の場合: コンテナイメージが ECR 等に登録済み

## クイックスタート

最も簡単な構成（EC2 + sqlite + Docker OpenSearch）で始める場合：

```bash
# 1. example をコピー
cp -r terraform/aws/examples/ragent-ec2-sqlite my-ragent
cd my-ragent

# 2. 変数ファイルを作成
cp terraform.tfvars.example terraform.tfvars
vi terraform.tfvars

# 3. デプロイ
terraform init
terraform plan
terraform apply
```

## 利用例

### ragent-ec2-complete — フル機能構成

EC2 上に全機能（Secrets Manager, OIDC 認証, Slack Bot, Vectorize）を含むデプロイ。本番環境の参考実装です。

```hcl
module "ragent" {
  source = "../../modules/ragent"

  compute_type     = "ec2"
  ragent_version   = "v1.0.0"
  instance_type    = "t4g.medium"
  root_volume_size = 30

  vpc_id     = "vpc-0123456789abcdef0"
  subnet_ids = ["subnet-aaa", "subnet-bbb"]

  vector_backend              = "sqlite"
  opensearch_mode             = "docker"
  secrets_manager_secret_name = "ragent/app"

  slack_bot_enabled          = true
  vectorize_enabled          = true
  vectorize_s3_source_bucket = "my-docs-bucket"
  vectorize_github_repos     = "my-org/repo1,my-org/repo2"

  mcp_auth_method      = "oidc"
  mcp_bypass_ip_ranges = ["10.0.0.0/8"]

  ragent_env = {
    BEDROCK_REGION       = "us-east-1"
    SLACK_SEARCH_ENABLED = "true"
  }

  tags = {
    Environment = "production"
    Project     = "ragent"
  }
}
```

### ragent-ec2-s3vectors — マネージドサービス構成

S3 Vectors と AWS OpenSearch を使用する本番向けの構成。

```hcl
module "ragent" {
  source = "../../modules/ragent"

  compute_type   = "ec2"
  ragent_version = "v1.0.0"

  vpc_id     = "vpc-0123456789abcdef0"
  subnet_ids = ["subnet-aaa", "subnet-bbb"]

  vector_backend        = "s3"
  s3vectors_bucket_name = "ragent-vectors"
  s3vectors_index_name  = "ragent-index"

  opensearch_mode = "aws"

  tags = { Environment = "production" }
}
```

### ragent-ec2-sqlite — 最小構成

外部依存なしの単一 EC2 デプロイ。開発・検証に最適。

```hcl
module "ragent" {
  source = "../../modules/ragent"

  compute_type   = "ec2"
  ragent_version = "v1.0.0"

  vpc_id     = "vpc-0123456789abcdef0"
  subnet_ids = ["subnet-aaa"]

  vector_backend  = "sqlite"
  opensearch_mode = "docker"

  tags = { Environment = "dev" }
}
```

### ragent-fargate-s3vectors — サーバーレス構成

Fargate で RAGent を実行。コンテナイメージの指定が必須。

```hcl
module "ragent" {
  source = "../../modules/ragent"

  compute_type        = "fargate"
  ragent_version      = "v1.0.0"
  container_image_uri = "123456789012.dkr.ecr.us-east-1.amazonaws.com/ragent:v1.0.0"

  vpc_id     = "vpc-0123456789abcdef0"
  subnet_ids = ["subnet-aaa", "subnet-bbb"]

  vector_backend        = "s3"
  s3vectors_bucket_name = "ragent-vectors"
  s3vectors_index_name  = "ragent-index"

  opensearch_mode = "aws"

  tags = { Environment = "production" }
}
```

## モジュール入力変数

### コンピュート

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `compute_type` | `string` | `"ec2"` | `ec2` または `fargate` |
| `instance_type` | `string` | `"t4g.medium"` | EC2 インスタンスタイプ |
| `ami_id` | `string` | `null` | AMI ID（null の場合は最新の AL2023 ARM64） |
| `key_name` | `string` | `null` | SSH 用キーペア名 |
| `root_volume_size` | `number` | `30` | ルート EBS ボリュームサイズ (GB) |
| `container_image_uri` | `string` | `null` | Fargate 用コンテナイメージ URI（Fargate 時必須） |
| `cpu` | `number` | `1024` | Fargate タスク CPU ユニット |
| `memory` | `number` | `2048` | Fargate タスクメモリ (MiB) |
| `desired_count` | `number` | `1` | Fargate タスク数 |
| `ragent_version` | `string` | — | RAGent リリースバージョン（例: `v1.0.0`）**必須** |

### ネットワーク

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `vpc_id` | `string` | — | VPC ID **必須** |
| `subnet_ids` | `list(string)` | — | コンピュート用サブネット ID **必須** |
| `opensearch_subnet_ids` | `list(string)` | `[]` | OpenSearch 用サブネット（空の場合は `subnet_ids` の先頭を使用） |

### ベクトル DB

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `vector_backend` | `string` | `"s3"` | `s3` または `sqlite` |
| `s3vectors_bucket_name` | `string` | `null` | S3 Vectors バケット名（`s3` 時必須） |
| `s3vectors_index_name` | `string` | `null` | S3 Vectors インデックス名（`s3` 時必須） |
| `s3vectors_region` | `string` | `"us-east-1"` | S3 Vectors リージョン |
| `s3vectors_dimension` | `number` | `1024` | ベクトル埋め込み次元数 |

### OpenSearch

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `opensearch_mode` | `string` | `"aws"` | `aws`（マネージド）または `docker`（EC2 内コンテナ） |
| `opensearch_domain_name` | `string` | `"ragent"` | OpenSearch ドメイン名 |
| `opensearch_engine_version` | `string` | `"OpenSearch_2.19"` | OpenSearch バージョン |
| `opensearch_instance_type` | `string` | `"or2.medium.search"` | OpenSearch インスタンスタイプ |
| `opensearch_volume_size` | `number` | `100` | OpenSearch EBS ボリュームサイズ (GB) |
| `opensearch_embedding_dimension` | `number` | `1024` | knn_vector フィールドの次元数 |
| `opensearch_index_name` | `string` | `"ragent"` | RAGent 用 OpenSearch インデックス名 |

### サービス

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `slack_bot_enabled` | `bool` | `true` | Slack Bot サービスを有効化 |
| `vectorize_enabled` | `bool` | `true` | Vectorize サービスを有効化 |
| `vectorize_s3_source_bucket` | `string` | `null` | Vectorize 用 S3 ソースバケット |
| `vectorize_github_repos` | `string` | `null` | Vectorize 対象 GitHub リポジトリ（カンマ区切り） |

### 認証

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `mcp_auth_method` | `string` | `"oidc"` | `ip`, `oidc`, `both`, `either` |
| `mcp_bypass_ip_ranges` | `list(string)` | `[]` | 認証バイパス CIDR レンジ |

### アプリケーション

| 変数 | 型 | デフォルト | 説明 |
|---|---|---|---|
| `bedrock_region` | `string` | `"us-east-1"` | Bedrock リージョン |
| `embedding_provider` | `string` | `"bedrock"` | `bedrock` または `gemini` |
| `ragent_env` | `map(string)` | `{}` | RAGent への追加環境変数 |
| `secrets_manager_secret_name` | `string` | `"ragent/app"` | Secrets Manager シークレット名 |
| `secrets_manager_region` | `string` | `null` | Secrets Manager リージョン（null でプロバイダーデフォルト） |
| `tags` | `map(string)` | `{}` | 全リソースへの追加タグ |

## モジュール出力

| 出力 | 説明 | 条件 |
|---|---|---|
| `ec2_instance_id` | EC2 インスタンス ID | EC2 モード |
| `ec2_private_ip` | EC2 プライベート IP | EC2 モード |
| `ecs_cluster_arn` | ECS クラスタ ARN | Fargate モード |
| `ecs_service_name` | ECS サービス名 | Fargate モード |
| `alb_dns_name` | ALB DNS 名 | 常時 |
| `alb_arn` | ALB ARN | 常時 |
| `mcp_endpoint` | MCP サーバーエンドポイント URL | 常時 |
| `opensearch_endpoint` | OpenSearch エンドポイント | `opensearch_mode = "aws"` |
| `opensearch_domain_arn` | OpenSearch ドメイン ARN | `opensearch_mode = "aws"` |
| `s3vectors_bucket_name` | S3 Vectors バケット名 | `vector_backend = "s3"` |
| `s3vectors_bucket_arn` | S3 Vectors バケット ARN | `vector_backend = "s3"` |
| `s3vectors_index_name` | S3 Vectors インデックス名 | `vector_backend = "s3"` |
| `s3vectors_index_arn` | S3 Vectors インデックス ARN | `vector_backend = "s3"` |
| `iam_role_arn` | RAGent 用 IAM ロール ARN | 常時 |
| `iam_role_name` | RAGent 用 IAM ロール名 | 常時 |
| `secrets_manager_secret_arn` | Secrets Manager シークレット ARN | 常時 |
| `secrets_manager_secret_name` | Secrets Manager シークレット名 | 常時 |

## 作成される AWS リソース

### 常に作成されるリソース

| リソース | 説明 |
|---|---|
| `aws_lb` | 内部 ALB（MCP エンドポイント） |
| `aws_lb_target_group` | ターゲットグループ（ポート 8080） |
| `aws_lb_listener` | HTTP リスナー（ポート 80） |
| `aws_security_group` (ALB) | ALB セキュリティグループ |
| `aws_security_group` (Compute) | コンピュート セキュリティグループ |
| `aws_secretsmanager_secret` | アプリケーションシークレット |
| `aws_secretsmanager_secret_version` | シークレット初期値 |

### EC2 モードで作成されるリソース

| リソース | 説明 |
|---|---|
| `aws_instance` | EC2 インスタンス (AL2023 ARM64) |
| `aws_iam_role` + `aws_iam_instance_profile` | EC2 用 IAM ロール・インスタンスプロファイル |
| `aws_lb_target_group_attachment` | ALB ターゲット登録 |

### Fargate モードで作成されるリソース

| リソース | 説明 |
|---|---|
| `aws_ecs_cluster` | ECS クラスタ |
| `aws_cloudwatch_log_group` | CloudWatch Logs (`/ecs/ragent`) |
| `aws_ecs_task_definition` | タスク定義（1〜3 コンテナ） |
| `aws_ecs_service` | ECS サービス (FARGATE) |
| `aws_iam_role` (task) | タスクロール |
| `aws_iam_role` (execution) | 実行ロール |

### `opensearch_mode = "aws"` で作成されるリソース

| リソース | 説明 |
|---|---|
| `aws_opensearch_domain` | OpenSearch ドメイン（VPC、暗号化、日本語アナライザー） |
| `aws_security_group` (OpenSearch) | OpenSearch セキュリティグループ |

### `vector_backend = "s3"` で作成されるリソース

| リソース | 説明 |
|---|---|
| `aws_s3vectors_vector_bucket` | S3 Vectors バケット |
| `aws_s3vectors_index` | S3 Vectors インデックス (cosine, float32) |

## セキュリティグループ

| セキュリティグループ | インバウンド | アウトバウンド |
|---|---|---|
| ALB | HTTP (80) `0.0.0.0/0` | TCP 8080 → Compute SG |
| Compute | TCP 8080 ← ALB SG | 全トラフィック |
| OpenSearch | HTTPS (443) ← Compute SG | — |

## IAM ポリシー

| ポリシー | アクション | 対象リソース | 条件 |
|---|---|---|---|
| `ragent-bedrock` | `bedrock:InvokeModel`, `bedrock:InvokeModelWithResponseStream`, `bedrock:Converse`, `bedrock:ConverseStream` | `*` | 常時 |
| `ragent-opensearch` | `es:ESHttp*` | OpenSearch ドメイン ARN | `opensearch_mode = "aws"` |
| `ragent-s3vectors` | `s3vectors:*` | S3 Vectors バケット・インデックス ARN | `vector_backend = "s3"` |
| `ragent-secrets-manager` | `secretsmanager:GetSecretValue`, `secretsmanager:DescribeSecret` | シークレット ARN | 常時 |
| `ragent-s3-source` | `s3:ListBucket`, `s3:GetObject` | ソースバケット ARN | `vectorize_s3_source_bucket` 指定時 |

## テンプレート（EC2 モード）

EC2 モードでは cloud-init と systemd テンプレートを使用してサービスを構成します。

| テンプレート | 用途 |
|---|---|
| `cloud-init.sh.tpl` | メイン user-data スクリプト: バイナリ DL、Docker インストール（必要時）、systemd サービス作成 |
| `ragent-init.sh.tpl` | OpenSearch インデックス初期化（kuromoji アナライザー、knn_vector マッピング） |
| `ragent-init.service.tpl` | OpenSearch 初期化用 one-shot systemd サービス |
| `ragent-mcp.service.tpl` | MCP サーバー systemd サービス（ポート 8080） |
| `ragent-slack.service.tpl` | Slack Bot systemd サービス（`slack_bot_enabled` 時） |
| `ragent-vectorize.service.tpl` | Vectorize systemd サービス（`--follow` モード、`vectorize_enabled` 時） |
| `ragent.env.tpl` | 環境変数ファイルテンプレート（Secrets Manager ブートストラップ） |

## テスト

`terraform test` でモジュールのバリデーションと構成を検証できます。

```bash
cd terraform/aws/modules/ragent
terraform init
terraform test
```

| テストファイル | 検証内容 |
|---|---|
| `validation.tftest.hcl` | 入力バリデーション: 無効な `compute_type`・`vector_backend`・`opensearch_mode`、無効なモード組み合わせの拒否 |
| `ec2_basic.tftest.hcl` | EC2 + S3 Vectors + AWS OpenSearch 構成の検証 |
| `ec2_sqlite.tftest.hcl` | EC2 + sqlite + Docker OpenSearch 構成の検証（cloud-init に OpenSearch 初期化スクリプト・knn_vector マッピング含む） |
| `fargate_basic.tftest.hcl` | Fargate + S3 Vectors + AWS OpenSearch 構成の検証 |

---

## Secrets Manager シークレット登録ガイド

RAGent が利用するシークレット（トークン・APIキー等）を AWS Secrets Manager に登録する手順です。

### 仕組み

RAGent は起動時に `SECRET_MANAGER_SECRET_ID` が設定されていると、Secrets Manager から JSON を取得し、**未設定の環境変数のみ**に値を注入します。既に設定済みの環境変数は上書きされません。

- JSON の各キーが環境変数名に対応します
- **文字列値のみ**が注入されます（オブジェクト・配列・数値・boolean は無視）
- ローカルの `export` や `.env` で設定した値が常に優先されます

### テンプレートファイル

[`secret-template.json`](secret-template.json) に登録可能な全キーの雛形を用意しています。

#### キー一覧

| キー | 説明 | 必須条件 |
|---|---|---|
| `AWS_BEARER_TOKEN_BEDROCK` | Bedrock Bearer Token | Bearer Token 認証を使用する場合 |
| `OPENSEARCH_ENDPOINT` | OpenSearch エンドポイント URL | 全コマンドで必須 |
| `OPENSEARCH_INDEX` | OpenSearch インデックス名 | 全コマンドで必須 |
| `SLACK_BOT_TOKEN` | Slack Bot トークン (`xoxb-`) | `slack-bot` コマンド |
| `SLACK_USER_TOKEN` | Slack User トークン (`xoxp-`) | 任意。設定時は `search:read` スコープを付与した user token で `search.messages` を呼び出します。未設定の場合は slack-bot のイベント由来 `action_token` がある経路のみ Bot Token (`xoxb-`) で `assistant.search.context` を呼び出せます |
| `SLACK_APP_TOKEN` | Slack App-level トークン (`xapp-`) | Socket Mode 利用時 |
| `OIDC_ISSUER` | OIDC プロバイダー URL | MCP Server OIDC 認証時 |
| `OIDC_CLIENT_ID` | OIDC クライアント ID | MCP Server OIDC 認証時 |
| `OIDC_CLIENT_SECRET` | OIDC クライアントシークレット | MCP Server OIDC 認証時 |
| `GITHUB_TOKEN` | GitHub Personal Access Token | プライベートリポジトリの vectorize |
| `GEMINI_API_KEY` | Google AI Studio API キー | `OCR_PROVIDER=gemini` or `EMBEDDING_PROVIDER=gemini` |
| `EMBEDDING_PROVIDER` | 埋め込みプロバイダー | `gemini` 使用時 |
| `EMBEDDING_MODEL` | 埋め込みモデル名 | カスタムモデル使用時 |
| `EMBEDDING_DIMENSION` | ベクトル次元数 | モデルデフォルト以外を使用する場合 |
| `OCR_PROVIDER` | OCR プロバイダー | PDF 処理時（`bedrock` or `gemini`） |
| `OCR_MODEL` | OCR モデル名 | カスタム OCR モデル使用時 |
| `CHAT_MODEL` | チャットモデル ID | カスタムモデル使用時 |

> **補足**: 不要なキーは JSON から削除するか、値を空文字列 `""` のままにしてください。空文字列のキーは環境変数に注入されますが、既に設定済みの環境変数は上書きされません。

### 登録手順

#### 1. テンプレートをコピーして値を設定

```bash
cp terraform/secret-template.json my-secrets.json
vi my-secrets.json
```

使用しないキーは削除してください。例えば Slack 連携が不要であれば `SLACK_*` 系のキーを削除します。

#### 2. AWS Secrets Manager に登録

**新規作成:**

```bash
aws secretsmanager create-secret \
  --name "ragent/app" \
  --description "RAGent application secrets" \
  --secret-string file://my-secrets.json \
  --region us-east-1
```

**既存シークレットの更新:**

```bash
aws secretsmanager put-secret-value \
  --secret-id "ragent/app" \
  --secret-string file://my-secrets.json \
  --region us-east-1
```

**AWS コンソールから登録:**

1. [AWS Secrets Manager コンソール](https://console.aws.amazon.com/secretsmanager/) を開く
2. **新しいシークレットを保存する** をクリック
3. **その他のシークレットのタイプ** を選択
4. **プレーンテキスト** を選び、`secret-template.json` の内容を貼り付けて値を編集
5. シークレット名（例: `ragent/app`）を設定してウィザードを完了

#### 3. 動作確認

```bash
# シークレットの内容を確認（値はマスクされません。ターミナルの取り扱いに注意）
aws secretsmanager get-secret-value \
  --secret-id "ragent/app" \
  --region us-east-1 \
  --query SecretString \
  --output text | jq .

# RAGent を起動してシークレットが読み込まれることを確認
# "loaded N secret(s) from Secrets Manager into environment" と表示されれば成功
ragent mcp-server
```

### IAM 権限

RAGent を実行する IAM ロールまたはユーザーに以下の権限が必要です:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:DescribeSecret"
      ],
      "Resource": "arn:aws:secretsmanager:<region>:<account-id>:secret:ragent/app-*"
    }
  ]
}
```

`<region>` と `<account-id>` は実際の値に置き換えてください。

> **Note**: Terraform モジュールを使用する場合、この IAM ポリシーは自動的に作成されます。手動設定は Terraform 外で RAGent を実行する場合にのみ必要です。

### ローカルでの上書き

特定のシークレットだけローカルで上書きしたい場合は、環境変数を直接設定します。Secrets Manager の値より優先されます。

```bash
export SECRET_MANAGER_SECRET_ID=ragent/app

# SLACK_BOT_TOKEN だけローカルの値を使い、他は Secrets Manager から取得
export SLACK_BOT_TOKEN=xoxb-local-override-token

ragent slack-bot
```
