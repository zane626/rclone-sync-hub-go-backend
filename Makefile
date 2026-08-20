.PHONY: install-swag fmt-check test vet vuln verify frontend-install frontend-audit frontend-build swagger swagger-only build run dev dev-mysql install-fresh docker-build

SWAG_VERSION ?= v1.16.6
GOVULN_VERSION ?= v1.7.0
FRESH_VERSION ?= v1.3.4

install-swag:
	go install github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION)

# 生成 Swagger 文档（解析 cmd/server/main.go 与 internal/api，输出到 cmd/server/docs）
swagger: install-swag
	swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal

# 仅生成文档，不安装 swag（要求本机已安装 swag）
swagger-only:
	swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal

build:
	go build -trimpath -o bin/server ./cmd/server

fmt-check:
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))"

test:
	go test -race ./...

vet:
	go vet ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULN_VERSION) ./...

frontend-install:
	corepack enable
	corepack prepare pnpm@9.0.0 --activate
	pnpm --dir frontend install --frozen-lockfile --registry=https://registry.npmjs.org

frontend-audit:
	pnpm --dir frontend audit --prod --audit-level=high --registry=https://registry.npmjs.org

frontend-build:
	pnpm --dir frontend build

verify: fmt-check test vet vuln frontend-audit frontend-build

docker-build:
	docker build -t rclone-sync-hub:local .

run:
	go run ./cmd/server

install-fresh:
	go install github.com/zzwx/fresh@$(FRESH_VERSION)

# 仅启动 MySQL（供本地 Fresh 开发用，与 config.dev.yaml 的 localhost:3306 对应）
dev-mysql:
	docker compose -f docker-compose.dev.yml up -d

# 开发模式：使用 config.dev.yaml + Fresh 热重载（需先 make dev-mysql 或本机已起 MySQL）
dev:
	CONFIG_PATH=configs/config.dev.yaml fresh
