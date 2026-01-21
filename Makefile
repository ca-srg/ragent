SHELL := /bin/bash

LINUX_ARM64_BIN := ./ragent-linux-arm64

# OpenSearch settings for local development
OPENSEARCH_VERSION ?= latest
OPENSEARCH_CONTAINER_NAME ?= opensearch-local
OPENSEARCH_PORT ?= 9200
OPENSEARCH_ADMIN_PASSWORD ?= Admin_123456

.PHONY: lint fmtvet build-linux-arm64 deploy-ec2 \
        opensearch-start opensearch-stop opensearch-restart opensearch-logs opensearch-status

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

## ============================================================================
## OpenSearch (Local Development)
## ============================================================================

## OpenSearchコンテナを起動（シングルノード、ローカル開発用、kuromoji付き）
opensearch-start:
	@echo "==> Starting OpenSearch container ($(OPENSEARCH_VERSION)) with kuromoji plugin"
	@if docker ps -a --format '{{.Names}}' | grep -q "^$(OPENSEARCH_CONTAINER_NAME)$$"; then \
		echo "Container $(OPENSEARCH_CONTAINER_NAME) already exists. Use 'make opensearch-restart' or 'make opensearch-stop' first."; \
		exit 1; \
	fi
	@docker run -d \
		--name $(OPENSEARCH_CONTAINER_NAME) \
		-p $(OPENSEARCH_PORT):9200 \
		-p 9600:9600 \
		-e "discovery.type=single-node" \
		-e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=$(OPENSEARCH_ADMIN_PASSWORD)" \
		-e "DISABLE_SECURITY_PLUGIN=true" \
		opensearchproject/opensearch:$(OPENSEARCH_VERSION)
	@echo "==> Installing kuromoji plugin..."
	@sleep 5
	@docker exec $(OPENSEARCH_CONTAINER_NAME) /usr/share/opensearch/bin/opensearch-plugin install --batch analysis-kuromoji
	@echo "==> Restarting OpenSearch to load kuromoji plugin..."
	@docker restart $(OPENSEARCH_CONTAINER_NAME)
	@echo "==> Waiting for OpenSearch to be ready..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12; do \
		sleep 5; \
		if curl -s http://localhost:$(OPENSEARCH_PORT)/_cluster/health | grep -q '"status"'; then \
			echo "==> OpenSearch is ready!"; \
			break; \
		fi; \
		echo "    Waiting... ($$i/12)"; \
	done
	@echo "==> OpenSearch started at http://localhost:$(OPENSEARCH_PORT)"
	@echo "    Plugins: kuromoji (Japanese analyzer)"
	@echo "    Security plugin is disabled for local development."

## OpenSearchコンテナを停止・削除
opensearch-stop:
	@echo "==> Stopping OpenSearch container"
	@docker stop $(OPENSEARCH_CONTAINER_NAME) 2>/dev/null || true
	@docker rm $(OPENSEARCH_CONTAINER_NAME) 2>/dev/null || true
	@echo "==> OpenSearch stopped"

## OpenSearchコンテナを再起動
opensearch-restart: opensearch-stop opensearch-start

## OpenSearchのログを表示
opensearch-logs:
	@docker logs -f $(OPENSEARCH_CONTAINER_NAME)

## OpenSearchの状態を確認
opensearch-status:
	@echo "==> Container status:"
	@docker ps -a --filter "name=$(OPENSEARCH_CONTAINER_NAME)" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
	@echo ""
	@echo "==> Health check:"
	@curl -s http://localhost:$(OPENSEARCH_PORT) 2>/dev/null | head -20 || echo "OpenSearch is not responding"
