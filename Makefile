SHELL := /bin/bash

.PHONY: lint fmtvet

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
