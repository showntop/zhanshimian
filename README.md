# 怎么打扮 · Monorepo

懂发型、妆容和穿搭的 **AI 形象顾问**。本仓库是全新实现（原型见 `appearance-coach-prototype/`，只读保留作参考）。

## 结构

```
apps/
  miniapp/    微信小程序（Taro 4 + React + TS + Vite）
  mobile/     手机端 iOS/Android（Expo + RN + Expo Router）· 二期骨架
  server/     Go 模块化单体（net/http + pgx + PostgreSQL 16，内嵌/独立 Worker）
packages/
  core/       @zsm/core  传输无关内核：API 客户端 / 任务轮询 / 媒体真实性契约 / 文案
  design/     @zsm/design 设计系统单源：tokens.ts → dist/{tokens.wxss, theme.ts}
contracts/
  openapi.yaml  前后端契约（唯一权威）
```

## 快速开始

```bash
make bootstrap     # pnpm install + go mod download
make up            # 起 PostgreSQL + API（127.0.0.1:58000）
make e2e           # 契约端到端回归（含越权/删除完整性）
make miniapp-dev   # Taro 开发构建（微信开发者工具打开 apps/miniapp）
make check         # 全量静态校验 + 编译 + 服务端测试
```

## 常用命令

| 目标 | 说明 |
|---|---|
| `make server-test / server-vet` | Go 单测 / 静态检查 |
| `make miniapp-build` | 小程序生产构建（dist/） |
| `make mobile-export` | 手机端 Metro 打包验证（无需真机） |
| `make design-build` | 重新生成 design 产物并校验无漂移 |
| `pnpm typecheck / lint` | 全仓 TS 类型 / lint |

## 核心约定

- **契约优先**：接口改动先改 `contracts/openapi.yaml`，两端同步；`/v1` 内只加字段，破坏性变更升 `/v2`。
- **数据真实性**：API 失败/为空必须渲染空态/错误态，绝不静默回退内置内容；内置模特图只能经 `exampleImage()` 且必须挂角标。
- **AI 标识**：AI 生成图一律显式标注（《人工智能生成合成内容标识办法》）。
- **异步任务统一模型**：创建返回 `202 + task`，轮询 `GET /v1/tasks/{id}`；间隔常量单源在 `@zsm/core`。
- 详细协作红线见 [AGENTS.md](./AGENTS.md)；架构决策见 `docs/architecture.md`。
