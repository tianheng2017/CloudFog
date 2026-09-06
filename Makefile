# 云之雾 B1 · 开发命令（Windows 下用 make.exe / wsl 均可；CI 在 Linux 执行）
SHELL := /bin/bash
BACKEND_DIR := backend
COMPOSE_FILE := deploy/docker-compose.dev.yml

.PHONY: dev compose-up compose-down build vet lint test-unit test-integration test-race migrate-up migrate-down migrate-status tidy

## 拉起本地存储（PG18 + Redis 8.10 + RabbitMQ 4.2），容器已存在且健康则跳过
compose-up:
	docker compose -f $(COMPOSE_FILE) up -d
	docker compose -f $(COMPOSE_FILE) ps

compose-down:
	docker compose -f $(COMPOSE_FILE) down

## 一键开发环境：存储 + 迁移 + 启动 all-in-one 进程
dev: compose-up
	cd $(BACKEND_DIR) && go run ./cmd/cloudfog migrate up
	cd $(BACKEND_DIR) && go run ./cmd/cloudfog --role=all

build:
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/cloudfog ./cmd/cloudfog

vet:
	cd $(BACKEND_DIR) && go vet ./...

lint:
	cd $(BACKEND_DIR) && golangci-lint run ./...

tidy:
	cd $(BACKEND_DIR) && go mod tidy

## 单元测试=无 tag 的默认文件；集成测试=//go:build integration（无 CLOUDFOG_RABBITMQ_URL 时自动 skip）
test-unit:
	cd $(BACKEND_DIR) && go test -count=1 ./...

test-integration:
	cd $(BACKEND_DIR) && go test -tags=integration -count=1 ./...

test-race:
	cd $(BACKEND_DIR) && go test -race ./...

migrate-up:
	cd $(BACKEND_DIR) && go run ./cmd/cloudfog migrate up

migrate-down:
	cd $(BACKEND_DIR) && go run ./cmd/cloudfog migrate down 1

migrate-status:
	cd $(BACKEND_DIR) && go run ./cmd/cloudfog migrate status
