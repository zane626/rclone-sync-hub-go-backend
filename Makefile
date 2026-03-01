# 项目 Makefile 示例
# 使用前请安装 swag: go install github.com/swaggo/swag/cmd/swag@latest
# 开发热重载请安装 Fresh: go install github.com/zzwx/fresh@latest

.PHONY: swagger build run dev dev-mysql install-fresh

# 安装 swag 命令行（需 Go 1.22+）
install-swag:
	go install github.com/swaggo/swag/cmd/swag@latest

# 生成 Swagger 文档（解析 cmd/server/main.go 与 internal/api，输出到 cmd/server/docs）
swagger: install-swag
	swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal

# 仅生成文档，不安装 swag（要求本机已安装 swag）
swagger-only:
	swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

# 安装 Fresh（热重载工具，兼容 Go 1.22）
install-fresh:
	go install github.com/zzwx/fresh@latest

# 仅启动 MySQL（供本地 Fresh 开发用，与 config.dev.yaml 的 localhost:3306 对应）
dev-mysql:
	docker compose up mysql -d

# 开发模式：使用 config.dev.yaml + Fresh 热重载（需先 make dev-mysql 或本机已起 MySQL）
dev:
	CONFIG_PATH=configs/config.dev.yaml fresh
