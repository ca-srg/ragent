# EC2 を利用したデバッグ手順

このドキュメントでは、RAGent の動作確認やインデックス調査を行う際に EC2 経由でデバッグする手順をまとめています。AWS Systems Manager Session Manager を利用して踏み台 EC2 に接続し、S3 Vector や OpenSearch の状態を確認する流れを想定しています。

## 1. 事前準備
- 調査対象の EC2 インスタンス ID (例: `i-0d6d46f15168a3ca8`)
- Session Manager を利用できる IAM ロールが付与されていること
- ローカルの AWS CLI で東京リージョン (`ap-northeast-1`) を利用可能であること

## 2. Session Manager でシェルを開く
基本コマンド:
```sh
aws ssm start-session --target i-0d6d46f15168a3ca8
```

### 2.1 対話コマンド実行ドキュメントの利用
Session 開始後すぐに切断される場合は、`AWS-StartInteractiveCommand` ドキュメントを利用してコマンドを指定します。
```sh
aws ssm start-session \
  --target i-0d6d46f15168a3ca8 \
  --document-name AWS-StartInteractiveCommand \
  --parameters '{"command":["bash -lc \"<実行したいコマンド>\""]}'
```
JSON 内のクォートが複雑になるので、必要に応じて `cat <<'EOF'` でスクリプトを生成してから実行します。

## 3. 環境情報の確認
調査に必要な環境変数は `/root/.bashrc` にまとまっています。Root 権限で内容を確認します。
```sh
aws ssm start-session \
  --target i-0d6d46f15168a3ca8 \
  --document-name AWS-StartInteractiveCommand \
  --parameters '{"command":["bash -lc \"sudo cat /root/.bashrc\""]}'
```
主な環境変数:
- `AWS_S3_VECTOR_BUCKET`
- `AWS_S3_VECTOR_INDEX`
- `AWS_S3_VECTOR_REGION`
- `OPENSEARCH_ENDPOINT`
- `OPENSEARCH_USERNAME`
- `OPENSEARCH_PASSWORD`

## 4. S3 Vector の状態確認
S3 Vector API は IAM 権限が必要です。以下のコマンドでインデックスやベクター一覧を取得できます。
```sh
aws s3vectors get-index \
  --vector-bucket-name rag \
  --index-name kibela

aws s3vectors list-vectors \
  --vector-bucket-name rag \
  --index-name kibela \
  --max-results 10
```
### 4.1 AccessDenied が出る場合
`AccessDeniedException` が返るときは、EC2 に付与されているロール (例: `RAG-ec2-ssm-role`) に以下のポリシーが不足しています。
- `s3vectors:GetIndex`
- `s3vectors:ListVectors`
- `s3vectors:QueryVectors`
- `s3:ListBucket`, `s3:GetObject` (必要に応じて)

権限追加後、再度コマンドを実行して状態を確認してください。

## 5. OpenSearch の状態確認
ドメインは Basic 認証が設定されていますが、**SigV4 サイン付きリクエストが必須** です。`curl` にユーザー名・パスワードを渡すだけでは `es:ESHttpGet` が拒否されます。

### 5.1 SigV4 付きリクエスト例
1. Session Manager 上でスクリプトを作成:
    ```sh
    aws ssm start-session \
      --target i-0d6d46f15168a3ca8 \
      --document-name AWS-StartInteractiveCommand \
      --parameters '{"command":["bash -lc \"cat <<'PY' > /tmp/opensearch_check.py\\nimport boto3\\nfrom botocore.awsrequest import AWSRequest\\nfrom botocore.auth import SigV4Auth\\nimport requests\\n\\nsession = boto3.Session()\\ncredentials = session.get_credentials()\\nregion = session.region_name or \\\"ap-northeast-1\\"\\ncreds = credentials.get_frozen_credentials()\\nurl = \\\"https://vpc-rag-amcwqbznt5pnjgizpc7yjwigcq.ap-northeast-1.es.amazonaws.com/_cat/indices?format=json\\"\\nrequest = AWSRequest(method=\\\"GET\\\", url=url)\\nSigV4Auth(creds, \\\"es\\\", region).add_auth(request)\\nprepared = request.prepare()\\nwith requests.Session() as s:\\n    response = s.send(prepared)\\n    print(response.status_code)\\n    print(response.text)\\nPY\""]}'
    ```
2. 作成したスクリプトを実行:
    ```sh
    aws ssm start-session \
      --target i-0d6d46f15168a3ca8 \
      --document-name AWS-StartInteractiveCommand \
      --parameters '{"command":["bash -lc \"python3 /tmp/opensearch_check.py\""]}'
    ```

### 5.2 代表的なエラーメッセージ
- `User: anonymous is not authorized to perform: es:ESHttpGet`: SigV4 署名が無い／失敗している
- `AccessDeniedException` (Describe 系 API): IAM ポリシー不足 (`es:DescribeDomain`, `es:DescribeElasticsearchDomain` など)

## 6. デバッグ時のチェックリスト
- [ ] Session Manager でインスタンスに接続できるか
- [ ] `/root/.bashrc` で必要な環境変数を取得したか
- [ ] S3 Vector API でインデックス一覧を取得し、対象ドキュメントが存在するか
- [ ] OpenSearch に SigV4 署名付きでアクセスできるか
- [ ] AccessDenied が発生した場合は IAM ポリシーの不足を確認したか

## 7. 追加のヒント
- 長いコマンドを実行する場合は、ローカルで文字列を Base64 化して `python3 -c "import base64;exec(...)"` 形式で渡すとクォートを簡素化できます。
- `sudo -n` を使うとパスワードなしで root 権限コマンドを実行できますが、権限不足の場合はエラーになるため注意してください。
- 調査終了後は Session Manager のセッションが自動で切断されるので、不要なプロセスが残らないことを確認してください。
