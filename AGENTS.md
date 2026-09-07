# AGENTS.md — 多 agent / 协作者工程规约

本仓库是「怎么打扮」的全新 monorepo 实现。`appearance-coach-prototype/` 是**只读参考**（原型视觉稿、旧实现、历史文档），任何改动不得进入该目录。

## 目录边界

- 各工作流只写自己的目录：`apps/miniapp`、`apps/mobile`、`apps/server`、`packages/*`、`contracts/`。
- 根级单点文件（package.json / tsconfig.base.json / Makefile / docker-compose.yml）已冻结；需要改动统一收敛到根级任务。
- 包名固定：`@zsm/core`、`@zsm/design`、`@zsm/miniapp`、`@zsm/mobile`；Go module `github.com/zhanshimian/server`。

## 产品红线（违反即返工）

1. **不打颜值分/身材分**、不身材羞辱、不做医学结论、不用警示红；一律用「可提升点」式尊重表达。
2. **AI 生成图像必须显式标识**：本人预览「AI 风格预览」、Demo「效果示例」、内置模特「风格参考」；`provider_version` 以 `demo` 开头的结果强制按示例处理。
3. **数据真实性**（移植自原型 `utils/media.js` 契约，实现在 `packages/core/src/media/truth.ts`）：
   - `lookImage(v)` 严格模式：无效 URL / webp 一律返回 `''`，绝不隐式回退内置图；
   - `userImage(v)` 用户照片无效时保持可见的空；
   - `exampleImage(slug, variant)` 是唯一返回内置模特图的入口，调用点必须叠 `.example-badge` + `.example-soft`；
   - Demo 内容只经服务端（`/v1/media/demo`、Demo Provider）进入，客户端绝不注入。
4. **错误态与内容不同屏**；空态/错误态必须给出下一步动作（重试/返回/重新拍摄）。
5. 文案红线：产品名「怎么打扮」；首页保留「你好，我是你的私人形象顾问」；场景叫「日常」不叫「通勤」。文案统一放 `packages/core/src/copy/zh.ts`。
6. 底部导航固定 `首页 / 方案 / 我的`，不加第四个 Tab；实验能力只进「体验实验室」。

## 小程序硬规则

- 图片**禁 WebP**（微信渲染空白）；包内资产只发 JPEG；tabBar/图标 PNG。
- 样式一律 `rpx`（`designWidth: 750`）；禁止裸 `px`（`apps/miniapp/scripts/check.mjs` 门禁）。
- 自定义组件设 `styleIsolation: 'apply-shared'`，共享语义类才能穿透。
- React 条件渲染禁 `{count && <View/>}`（0 会被渲染出来）；列表 key 用资源 ID 不用下标。
- tab 页防重复加载：`useDidShow` + ref 标志；切 tab 缓存优先渲染、后台校验，不清空已渲染图片（防闪屏）。
- 轮询：只用 `@zsm/core` 的 `useTaskPolling`（间隔单源），页面隐藏即停，失败 5 次进失败态。
- 微信 3.17+ 拒绝 `http://` 图：开发环境启用 `localizeDevImages` 中间件（仅非 https 时）。

## 服务端硬规则

- 分层：`httpapi`（协议翻译）→ `service`（业务+事务）→ `repository`（每次读写校验 资源ID+user_id，越权一律 404）/ `provider`（隔离一切外部）/ `storage`。禁止 handler 直查数据库。
- 异步任务一律走统一 `tasks` 表 + `ClaimTask`（SKIP LOCKED）；新增任务类型 = 注册 handler，不改循环。
- AI 只经能力路由（`ai-routing.*.json`），业务代码不出现厂商/模型名。
- 迁移只向前（embed + 按文件名序）；`APP_ENV=production` 门禁全开。

## 验证（提交前必跑）

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

动效改动需对照 `packages/design/src/motion.ts` 参数；`prefers-reduced-motion` 必须归零处理。
