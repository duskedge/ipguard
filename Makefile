# ipguard 开发与构建。
GOTOOLCHAIN := local
export GOTOOLCHAIN

VERSION   ?= dev
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)

# 本地开发用的环境（日志目录、GeoIP 库、CrowdSec 凭据）放在未跟踪的 .env.local，例如：
#   IPGUARD_AUTH_PASSWORD=devpass
#   IPGUARD_LOGS_DIR=/path/to/logs
#   IPGUARD_GEOIP_CITYDB=/path/to/dbip-city-lite.mmdb
#   IPGUARD_CROWDSEC_URL=http://192.168.1.8:8099
-include .env.local
export

.PHONY: help
help:
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | awk -F'\t' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## 启动后端（读 .env.local）
	CGO_ENABLED=0 go run ./cmd/ipguard

.PHONY: build
build: ## 构建二进制
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/ipguard ./cmd/ipguard

.PHONY: web-dev
web-dev: ## 前端开发服务器（代理 /api 到 localhost:8080）
	cd web && pnpm dev

.PHONY: web-embed
web-embed: ## 构建前端并放进嵌入目录（之后 make build 得到单文件二进制）
	@cd web && pnpm build
	@rm -rf internal/webui/dist
	@cp -r web/dist internal/webui/dist
	@echo "→ 前端产物已就位；提交前执行 make web-embed-reset"

.PHONY: web-embed-reset
web-embed-reset: ## 还原嵌入目录为占位页（避免把产物误提交）
	@rm -rf internal/webui/dist
	@git checkout -- internal/webui/dist
	@echo "→ 已还原为占位页"

.PHONY: test
test: ## 后端测试 + 前端类型检查与单测
	go test ./... -race -count=1
	cd web && pnpm exec vue-tsc --noEmit && pnpm exec vitest run

.PHONY: lint
lint: ## 静态检查
	go vet ./...
	@gofmt -l ./cmd ./internal | grep . && { echo "✗ 以上文件未格式化，执行 make fmt"; exit 1; } || echo "✓ gofmt 通过"

.PHONY: fmt
fmt: ## 格式化
	gofmt -w ./cmd ./internal ./migrations

.PHONY: geoip
geoip: ## 下载 dbip-city-lite（当月，没出则取上月）与 ip2region 到 data/geoip/
	@mkdir -p data/geoip
	@m=$$(date +%Y-%m); curl -fSL "https://download.db-ip.com/free/dbip-city-lite-$$m.mmdb.gz" -o data/geoip/city.mmdb.gz \
	  || { m=$$(date -v-1m +%Y-%m 2>/dev/null || date -d '1 month ago' +%Y-%m); curl -fSL "https://download.db-ip.com/free/dbip-city-lite-$$m.mmdb.gz" -o data/geoip/city.mmdb.gz; }
	@gunzip -f data/geoip/city.mmdb.gz && mv data/geoip/city.mmdb data/geoip/dbip-city-lite.mmdb
	@curl -fSL https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb -o data/geoip/ip2region.xdb
	@ls -lh data/geoip/

.PHONY: docker
docker: ## 本地构建镜像
	docker build -f deploy/Dockerfile --build-arg APP_VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t ipguard:dev .
