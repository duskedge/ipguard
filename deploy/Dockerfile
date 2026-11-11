# ipguard 镜像（多阶段、支持多平台）。在仓库根目录构建：
#   docker build -f deploy/Dockerfile -t ipguard .
#   docker buildx build -f deploy/Dockerfile --platform linux/amd64,linux/arm64 -t ipguard .
#
# 前端打包、GeoIP 下载、Go 编译都跑在构建机原生平台（$BUILDPLATFORM），Go 交叉编译到目标架构；
# 只有最后的运行层按目标平台组装，所以多平台构建不需要在 QEMU 里跑 pnpm / go。
#
# 国内网络可通过构建参数换源，例如：
#   --build-arg NPM_REGISTRY=https://registry.npmmirror.com \
#   --build-arg GOPROXY=https://goproxy.cn,direct \
#   --build-arg APK_MIRROR=mirrors.tuna.tsinghua.edu.cn

ARG NODE_VERSION=22
ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.20

# ── Stage 1: 构建前端 ────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
ARG NPM_REGISTRY=""
WORKDIR /web
RUN corepack enable && corepack prepare pnpm@10 --activate
# 依赖层单独缓存：只有 lock 文件变了才重新装
COPY web/package.json web/pnpm-lock.yaml ./
RUN if [ -n "$NPM_REGISTRY" ]; then pnpm config set registry "$NPM_REGISTRY"; fi \
    && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm exec vite build


# ── Stage 2: 下载 GeoIP 库 ───────────────────────────────────────────────────
# dbip-city-lite（全球城市级 + 坐标，CC BY 4.0）：每月初发布，当月文件没出来就退回上个月。
# ip2region（国内省市 + 运营商，修正 dbip 对国内运营商网段的误判）：GitHub，失败再试 Gitee。
# 都下不到也不让构建失败：运行时降级，可以在 data/geoip/ 放一份补上。
FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS geoip
ARG GEOIP_PROXY=""
RUN mkdir -p /geoip && cd /geoip \
    && export https_proxy="${GEOIP_PROXY}" \
    && cur="$(date +%Y-%m)" \
    && prev="$(date -d @$(( $(date +%s) - 32*86400 )) +%Y-%m)" \
    && for m in "$cur" "$prev"; do \
         if wget -q -T 120 -O city.mmdb.gz "https://download.db-ip.com/free/dbip-city-lite-${m}.mmdb.gz"; then \
           gunzip -f city.mmdb.gz && mv city.mmdb dbip-city-lite.mmdb && echo "GeoIP: dbip-city-lite-${m}" && break; \
         fi; \
       done; \
    for u in "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb" \
             "https://gitee.com/lionsoul/ip2region/raw/master/data/ip2region_v4.xdb"; do \
         if wget -q -T 120 -O ip2region.xdb "$u" && [ "$(wc -c < ip2region.xdb)" -gt 1000000 ]; then \
           echo "ip2region: $u" && break; \
         fi; \
         rm -f ip2region.xdb; \
       done; \
    ls -la /geoip


# ── Stage 3: 构建后端（前端产物嵌进二进制，交叉编译到目标架构）────────────
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder
ARG GOPROXY=""
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN if [ -n "$GOPROXY" ]; then export GOPROXY="$GOPROXY"; fi && go mod download
COPY . .
# 覆盖仓库里的占位页。先清空：Vite 产物文件名带 hash，叠加会把旧 chunk 一起嵌进去
RUN rm -rf internal/webui/dist
COPY --from=web /web/dist ./internal/webui/dist
ARG APP_VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
# SQLite 用 glebarez 纯 Go 实现，静态编译不需要 CGO
RUN if [ -n "$GOPROXY" ]; then export GOPROXY="$GOPROXY"; fi \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w \
      -X main.version=${APP_VERSION} \
      -X main.gitCommit=${GIT_COMMIT} \
      -X main.buildTime=${BUILD_TIME}" \
    -o ipguard ./cmd/ipguard


# ── Stage 4: 运行时 ─────────────────────────────────────────────────────────
FROM alpine:${ALPINE_VERSION}
ARG APK_MIRROR=""
RUN if [ -n "$APK_MIRROR" ]; then sed -i "s/dl-cdn.alpinelinux.org/${APK_MIRROR}/g" /etc/apk/repositories; fi \
    && apk add --no-cache ca-certificates tzdata wget
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /app/ipguard /app/ipguard
COPY --from=geoip /geoip/ /app/geoip/
# data 下放 SQLite（审计日志）、JWT 密钥、可选的按月更新 geoip/，必须挂出去持久化
RUN mkdir -p /app/data /logs
VOLUME ["/app/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
LABEL org.opencontainers.image.source="https://github.com/duskedge/ipguard" \
      org.opencontainers.image.description="Self-hosted access-log analytics, GeoIP insights and one-click CrowdSec banning" \
      org.opencontainers.image.licenses="MIT"
ENTRYPOINT ["/app/ipguard"]
