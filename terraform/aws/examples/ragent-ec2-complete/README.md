# RAGent EC2 Complete Example

EC2 上に RAGent をフル機能構成でデプロイする例です。

## 構成

| 項目 | 設定 |
|---|---|
| コンピュート | EC2 (ARM64, t4g.medium) |
| ベクトル DB | sqlite-vec (ローカル) |
| OpenSearch | Docker (EC2 内コンテナ) |
| シークレット管理 | AWS Secrets Manager |
| MCP 認証 | OIDC + バイパス IP |
| Slack Bot | 有効 |
| Vectorize | S3 ソース + GitHub リポジトリ |

## 前提条件

- VPC とプライベートサブネットが作成済みであること
- Secrets Manager にシークレット (`ragent/app`) を登録済みであること
- OIDC プロバイダ（Google Workspace 等）の設定が完了していること

## 使い方

```bash
# 1. 変数ファイルをコピーして編集
cp terraform.tfvars.example terraform.tfvars
vi terraform.tfvars

# 2. 初期化
terraform init

# 3. プラン確認
terraform plan

# 4. デプロイ
terraform apply
```

## Secrets Manager に登録するシークレット

`ragent/app` に以下の JSON を登録してください：

```json
{
  "SLACK_BOT_TOKEN": "xoxb-...",
  "SLACK_USER_TOKEN": "xoxp-...",
  "OIDC_CLIENT_ID": "...",
  "OIDC_CLIENT_SECRET": "...",
  "GITHUB_TOKEN": "ghp_..."
}
```

## Outputs

| 名前 | 説明 |
|---|---|
| `ec2_instance_id` | EC2 インスタンス ID |
| `ec2_private_ip` | EC2 プライベート IP |
| `alb_dns_name` | ALB DNS 名 |
| `mcp_endpoint` | MCP サーバーエンドポイント URL |
| `iam_role_arn` | RAGent 用 IAM ロール ARN |
| `secrets_manager_secret_arn` | Secrets Manager シークレット ARN |
