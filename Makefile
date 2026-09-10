# 「UP一下」monorepo 唯一构建/验证入口
# Go 与容器操作统一走 Make；TS 侧由 pnpm workspace 编排。

.PHONY: bootstrap up down logs e2e server-test server-vet server-build server-tidy \
        miniapp-dev miniapp-build miniapp-check mobile-start mobile-export \
        design-build typecheck lint check

bootstrap:
	pnpm install
	cd apps/server && go mod download

# ---------- 服务端（Go + PostgreSQL） ----------
up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f api

server-test:
	cd apps/server && go test ./...

server-vet:
	cd apps/server && go vet ./...

server-build:
	cd apps/server && CGO_ENABLED=0 go build -o /tmp/zhanshimian-api ./cmd/api

server-tidy:
	cd apps/server && go mod tidy

# e2e：起本地 compose，等 healthz 就绪后跑契约回归
e2e:
	$(MAKE) up
	@until curl -fsS http://127.0.0.1:58000/healthz >/dev/null 2>&1; do \
		echo "waiting for api..."; sleep 1; \
	done
	bash apps/server/scripts/e2e.sh

# ---------- 小程序（Taro） ----------
miniapp-dev:
	pnpm --filter @zsm/miniapp dev:weapp

miniapp-build:
	pnpm --filter @zsm/miniapp build:weapp

miniapp-check:
	pnpm --filter @zsm/miniapp typecheck
	pnpm --filter @zsm/miniapp build:weapp
	node apps/miniapp/scripts/check.mjs

# ---------- 手机端（Expo） ----------
mobile-start:
	pnpm --filter @zsm/mobile start

mobile-export:
	pnpm --filter @zsm/mobile typecheck
	pnpm --filter @zsm/mobile export

# ---------- 共享包与聚合 ----------
design-build:
	pnpm --filter @zsm/design build
	@git diff --exit-code packages/design/dist \
		|| (echo "packages/design/dist 与 tokens.ts 漂移，请重新生成并提交"; exit 1)

typecheck:
	pnpm -r run typecheck

lint:
	pnpm -r run lint

check: typecheck lint design-build miniapp-check mobile-export server-vet server-test
	pnpm --filter @zsm/core test
	@echo "=== ALL CHECKS PASSED ==="
