# 验收清单（WS-7 总验收）

> 编译/静态/e2e 通过 ≠ 运行时验证通过。本清单区分两者，未验证项必须如实标注。

## A. 构建与静态（机器可判）

- [x] `pnpm install` 干净环境一次通过（lockfile 已提交）—— 本机 2026-09-08 通过；lockfile 待随 WS-5/6 提交
- [x] `pnpm typecheck` / `pnpm lint` 全仓 0 错误 —— `make check` 2026-09-08 通过
- [x] `make design-build` 产物无漂移（dist 与 tokens 一致）
- [x] `pnpm --filter @zsm/miniapp build:weapp` 产出 dist/（app.json、2 分包、jpg 资产齐全）
- [x] `node apps/miniapp/scripts/check.mjs` 通过（主包 1.51MB / 上限 1.6MB）
- [x] `make server-vet` / `make server-test` 全绿
- [x] `make e2e` 全绿（新契约全流程 + 幂等 PUT + 单方案重试 + sms 合并 + 删除完整性 + 越权 404）—— 2026-09-08 本地 compose 通过
- [x] `contracts` lint 通过；`check-sync.mjs` openapi ↔ core 端点表一致（49 路径 / 52 操作配对）
- [x] `pnpm --filter @zsm/mobile typecheck && export` 通过（Metro 打包验证）

## B. 功能对齐（对照原型 18 页 + 差距 G1-G7）

- [x] 主闭环 7 页 + 首页双状态（新用户/回访）+ scene + profile-setup（G1）—— 小程序已实现；手机端已接线
- [x] plans 三方案轮播+对比+生成轮询；plan 详情+发型师参考卡（G2）—— 小程序已实现
- [x] checklist 乐观勾选；feedback 成功态个性化文案（G3）—— 小程序已实现
- [x] tools 4 页（hair 900ms 轮询/outfit/purchase/lab 占位）
- [x] life 4 页（today/wardrobe/advisor/share）
- [x] token 全部品牌值（G4）；分析文案服务端 stage 驱动（G7）—— 代码路径已对齐，真机未验

## C. 运行时（人工/真机，如实标注状态）

- [ ] 微信开发者工具打开 apps/miniapp 预览三 tab 与分包页跳转 —— 状态：**未验证**
- [ ] 真机：登录、三图上传、扫描动效、方案对比滑块手感 —— 状态：**未验证**
- [ ] Expo：iOS 模拟器/真机登录+主闭环走通 —— 状态：**未验证**（仅 Metro export 通过）
- [x] e2e 已覆盖的部分（契约行为）视为已验证 —— 2026-09-08 重跑通过

## D. 工程卫生

- [x] `appearance-coach-prototype/` 零新增改动
- [x] `git log --follow apps/server/cmd/api/main.go` 历史连续
- [x] 分 WS 提交，mv 与 module 改名独立提交
- [x] 媒体契约单测在 `packages/core/tests/truth.test.mjs`；`pnpm --filter @zsm/core test` 31 通过，已纳入 `make check`
