# 架构决策记录（ADR 摘要）

> 本文档记录「uplook」monorepo 重实现的的关键架构决策与理由。数值规范（token/动效/接口表）见 `appearance-coach-prototype/docs/implementation-plan.md`（只读参考）；契约见 `contracts/openapi.yaml`。

## 1. 总体拓扑

```
微信小程序(Taro 4)      手机端(Expo RN, 二期)
        │                      │
        └──────────┬───────────┘
                   ▼
          Go 模块化单体（apps/server）
   httpapi → service → repository / provider / storage
                   │
     PostgreSQL 16（业务+统一任务队列） 私有COS（照片）  AI/微信/天气/短信 Provider
                   │
          Worker（tasks 表 SKIP LOCKED，可独立部署）
```

一套服务端多端复用；单库任务队列在单日任务量到达瓶颈前不引入消息中间件（拆分触发条件沿用 technical-design §11）。

## 2. 关键决策

| # | 决策 | 理由 | 替代方案（否决原因） |
|---|---|---|---|
| D1 | 小程序用 **Taro 4 + React + TS + Vite** | 与 React 原型同构（视觉/逻辑可翻译）；与手机端共享 `@zsm/core`+`@zsm/design`；自动分包与热更新 | 原生无编译链（无法共享代码）；uni-app+Vue（与原型异构） |
| D2 | 服务端 **Go 代码资产迁移 + 契约/架构重构** | 已验证的登录/存储/AI 路由/越权校验直接复用；41→49 端点契约全新设计（客户端全新，零兼容包袱） | TS 重写（2-3 周回归风险，无对应收益） |
| D3 | **统一任务系统**（tasks 表 + 单认领循环 + handler 注册表） | 旧版 4 套队列代码重复（DRY 违例）；新增任务类型=注册 handler（OCP）；统一轮询端点 `GET /v1/tasks/{id}` | 保留 4 套（重复代码、4 种轮询语义） |
| D4 | **BFF 聚合端点**（/v1/home/bootstrap、plans 内嵌 look_task） | 首页 4 次并发请求并 1 次；方案页免二次查询任务状态 | 纯资源端点（客户端拼装，请求次数×3） |
| D5 | 设计系统 **tokens 单源**（tokens.data.mjs → 生成 wxss/theme.ts） | 双端视觉一致性的机械保证；dist 入库+CI 防漂移 | 两端各写一份（必然漂移） |
| D6 | 样式 **CSS variables + SCSS，不引 Tailwind** | 「少量稳定 token+语义类」的设计语言；原子类在小程序样式隔离/rpx/分包体积上全是负收益 | Tailwind JIT |
| D7 | 编排用 **pnpm scripts + Makefile**，不引 turbo | 依赖图深度 1；Go/compose 本就不归 turbo；引入=维护两套编排 | turbo/nx |
| D8 | 多端身份 **user_identities(provider,identifier)** | 小程序↔手机端同一用户数据互通（二期核心价值）；存量 open_id 一次性回填 | 每端独立用户（数据孤岛） |
| D9 | 会话保留 **随机 token+SHA-256 摘要**（无 JWT） | 吊销=删行；无签名密钥轮换问题；滑动续期补足长活 | JWT（吊销复杂） |
| D10 | 手机端 **Expo SDK 54+ + Expo Router** | 原型 spring/手势/BottomSheet 参数 1:1 映射 Reanimated；EAS Update 免审热修 | Taro RN（成熟度低）；双原生（成本） |

## 3. 兼容性 Pin（已知且受控）

- **Taro + React 19**：Taro weapp 渲染层基于 react-reconciler；若最新组合运行异常，pin React 18.3.1 并在此记录解锁条件（Taro 官方 React 19 支持稳定后解除）。WS-5 以 1 页 spike 首先验证。
- **px/rpx**：designWidth 750 + scss 只写 rpx；design 包源码存 px，仅生成器 ×2。
- **Vite 资产内联**：`assetsInlineLimit: 0` + copy.patterns——保 `/assets/*.jpg` 绝对路径契约（媒体真实性逻辑的前提）。

## 4. 请求纪律（性能与体验约定）

1. 首页单次 bootstrap；仅在有活跃任务且页面可见时批量轮询（1.5s）。
2. 分析页轮询 tasks/{id} 700ms，onHide 即停，≥90% 预取报告。
3. 方案页列表内嵌任务状态，仅在有进行中任务时 2500ms 轮询自身端点。
4. Tab 切换缓存优先+后台校验（防闪屏、防重复请求）。
5. 三图并行上传+单图进度；失败单图重试。
6. 401 单飞重登重放一次（防并发登录风暴）。

## 5. 目录与边界

- `apps/miniapp`（Taro）：主包 11 页 + tools/life 分包；组件/services 只依赖 `@zsm/core`+`@zsm/design`。
- `apps/mobile`（Expo）：本期登录+主闭环 7 页，复访页占位。
- `apps/server`（Go）：httpapi 按域拆文件；迁移只向前；生产门禁全开。
- `packages/core`：传输无关内核（零依赖）；平台差异全部经接口注入（HttpAdapter/TokenStore/LocalLooksResolver/subscribeVisibility）。
- `packages/design`：token 单源 + 生成产物（入库防漂移）。
- `contracts/openapi.yaml`：契约唯一权威；check-sync 与 core 端点表互锁。
