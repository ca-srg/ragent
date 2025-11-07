SHELL := /bin/bash

LINUX_ARM64_BIN := ./ragent-linux-arm64

.PHONY: lint fmtvet build-linux-arm64 deploy-ec2

## go fmt と go vet のみ（CI で使用）
fmtvet:
	@echo "==> go fmt"
	@go fmt ./...
	@echo "==> go vet"
	@go vet ./...

## ローカル向け: fmt/vet + golangci-lint
lint: fmtvet
	@echo "==> golangci-lint"
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint が見つかりません。インストール手順: https://golangci-lint.run/"; exit 1; }
	@golangci-lint run

## Linux/arm64 向けの単体バイナリをビルド（デプロイ前提）
build-linux-arm64:
	@echo "==> GOOS=linux GOARCH=arm64 go build"
	@GOOS=linux GOARCH=arm64 go build -o $(LINUX_ARM64_BIN) .

## ローカルビルド済みバイナリを指定EC2 (via SSM) の /root/ragent に配置
deploy-ec2: build-linux-arm64
	@[ -n "$(INSTANCE_ID)" ] || { echo "INSTANCE_ID を指定してください (例: make deploy-ec2 INSTANCE_ID=i-xxxxxxxx)"; exit 1; }
	@echo "==> Uploading binary to temporary storage"
	@tmp_url=$$(curl -s -F "reqtype=fileupload" -F "fileToUpload=@$(LINUX_ARM64_BIN)" https://catbox.moe/user/api.php); \
	 if [ -z "$$tmp_url" ]; then echo "バイナリのアップロードに失敗しました"; exit 1; fi; \
	 echo "Uploaded: $$tmp_url"; \
	 backup_ts=$$(date +%s); \
	 payload=$$(TMP_URL="$$tmp_url" INSTANCE_ID="$(INSTANCE_ID)" BACKUP_TS="$$backup_ts" python3 -c "import json, os;url=os.environ['TMP_URL'].strip();instance_id=os.environ['INSTANCE_ID'].strip();backup=os.environ['BACKUP_TS'].strip();command=('bash -lc \"set -euo pipefail; if [ -f /root/ragent ]; then mv /root/ragent /root/ragent.bak.{backup}; fi; curl -fL {url!r} -o /root/ragent; chmod +x /root/ragent\"').format(backup=backup, url=url);payload={'DocumentName':'AWS-RunShellScript','Targets':[{'Key':'instanceids','Values':[instance_id]}],'Comment':'deploy ragent binary','Parameters':{'commands':[command]}};print(json.dumps(payload))"); \
	 echo "==> Executing SSM send-command"; \
	 cmd_id=$$(aws ssm send-command --cli-input-json "$$payload" --query 'Command.CommandId' --output text); \
	 echo "SSM CommandId: $$cmd_id"; \
	 aws ssm wait command-executed --command-id $$cmd_id --instance-id "$(INSTANCE_ID)"; \
	 status=$$(aws ssm list-command-invocations --command-id $$cmd_id --details --query 'CommandInvocations[0].CommandPlugins[0].StatusDetails' --output text); \
	 echo "Command status: $$status"; \
	 if [ "$$status" != "Success" ]; then exit 1; fi
