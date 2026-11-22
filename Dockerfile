FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder

WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY src ./src
COPY public ./public
COPY index.html ./
COPY vite.config.ts ./
COPY tsconfig*.json ./
COPY tailwind.config.ts ./
COPY postcss.config.js ./
COPY components.json ./
COPY eslint.config.js ./

RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS go-builder

WORKDIR /src/server

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
COPY --from=frontend-builder /app/dist ./internal/webui/dist

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/ovh-nexus ./cmd/ovh-nexus

FROM alpine:3.24

LABEL org.opencontainers.image.title="OVH Nexus" \
      org.opencontainers.image.description="Self-hosted OVHcloud control, automated bare-metal/VPS procurement & privacy-shielded MCP AI gateway console" \
      org.opencontainers.image.url="https://ovh-nexus.ellery.me" \
      org.opencontainers.image.documentation="https://ovh-nexus.ellery.me" \
      org.opencontainers.image.source="https://github.com/ElleryHe/OVH-Nexus" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

RUN apk add --no-cache ca-certificates tzdata su-exec && \
    addgroup -g 65532 -S nexus && \
    adduser -u 65532 -S -G nexus nexus && \
    mkdir -p /data && \
    chown -R 65532:65532 /data

COPY --from=go-builder /out/ovh-nexus /usr/local/bin/ovh-nexus
COPY --chmod=0755 docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Boot entrypoint initializes /data ownership (PUID:PGID) and drops privileges via su-exec
ENV PORT=8080 \
    DATA_DIR=/data \
    TZ=UTC \
    PUID=65532 \
    PGID=65532 \
    GOMEMLIMIT=256MiB \
    GOGC=50

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
