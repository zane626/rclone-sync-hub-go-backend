# ---------- 阶段 1: 构建 Vue3 ----------
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci --legacy-peer-deps 2>/dev/null || npm install --legacy-peer-deps

COPY frontend/ ./
RUN npm run build 2>/dev/null || true
# 若 frontend 暂无 build 脚本，则保留占位 dist
RUN mkdir -p dist && [ -f dist/index.html ] || echo '<!DOCTYPE html><html><body>Rclone Sync Hub</body></html>' > dist/index.html

# ---------- 阶段 2: 构建 Go ----------
FROM golang:1.22-alpine AS go-builder
WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .

# 将 Vue 构建产物复制到 cmd/server/frontend/dist 供 go:embed 使用
COPY --from=frontend-builder /app/frontend/dist ./cmd/server/frontend/dist

# 安装 swag 并生成 Swagger 文档（再编译，使文档随镜像发布；生产可通过 config 关闭路由）
RUN go install github.com/swaggo/swag/cmd/swag@latest \
	&& swag init -g cmd/server/main.go -o cmd/server/docs --parseDependency --parseInternal

RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

# ---------- 阶段 3: 最终镜像 ----------
FROM alpine:3.19
RUN apk add --no-cache ca-certificates rclone

WORKDIR /app
COPY --from=go-builder /server .
COPY configs/config.yaml ./configs/

EXPOSE 8080
ENV CONFIG_PATH=/app/configs/config.yaml
ENTRYPOINT ["/app/server"]
