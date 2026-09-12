# syntax=docker/dockerfile:1.7

# Frontend dependencies and runtimes are pinned so the same source produces a
# repeatable artifact. Build failures intentionally fail the image build.
ARG NODE_VERSION=24.19.0
ARG GO_VERSION=1.26.7
# Static frontend assets are identical for both target architectures.
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS frontend-builder
WORKDIR /src/frontend

RUN corepack enable && \
    for attempt in 1 2 3; do \
      if corepack prepare pnpm@9.0.0 --activate; then exit 0; fi; \
      if [ "$attempt" -eq 3 ]; then exit 1; fi; \
      echo "pnpm download failed (attempt $attempt/3), retrying in 5 seconds" >&2; \
      sleep 5; \
    done
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS go-builder
WORKDIR /src

RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
# Keep verified modules in the layer: cache mounts are not part of exported
# layers and were unavailable to the subsequent compilation step.
RUN go mod download && go mod verify
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./cmd/server/frontend/dist

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/rclone-sync-hub ./cmd/server

FROM rclone/rclone:1.75.0 AS rclone

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app \
    && mkdir -p /app/configs /config/rclone \
    && chown -R 10001:10001 /app /config

WORKDIR /app
COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/rclone
COPY --from=go-builder /out/rclone-sync-hub /app/server
COPY --chown=10001:10001 configs/config.yaml /app/configs/config.yaml

USER 10001:10001
EXPOSE 8080
ENV CONFIG_PATH=/app/configs/config.yaml \
    AUTH_ENABLED=true \
    ENABLE_SWAGGER=false \
    RCLONE_CONFIG=/config/rclone/rclone.conf \
    HOME=/tmp \
    XDG_CACHE_HOME=/tmp/rclone-cache

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health/ready || exit 1

STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/server"]
