# 验收清单（WS-7 总验收）

> 编译/静态/e2e 通过 ≠ 运行时验证通过。本清单区分两者，未验证项必须如实标注。

## A. 构建与静态（机器可判）

- [ ] `pnpm install` 干净环境一次通过（lockfile 已提交）
- [ ] `pnpm typecheck` / `pnpm lint` 全仓 0 错误
- [ ] `make design-build` 产物无漂移（dist 与 tokens 一致）
- [ ] `pnpm --filter @zsm/miniapp build:weapp` 产出 dist/（app.json、2 分包、jpg 资产齐全）
- [ ] `node apps/miniapp/scripts/check.mjs` 通过（路由↔目录、三件套、资产存在、无 webp/png 母版、scss 无裸 px、Image 必经媒体契约、无字面量 http:// 图、主包 ≤1.6MB）
- [ ] `make server-vet` / `make server-test` 全绿
- [ ] `make e2e` 全绿（新契约全流程 + 幂等 PUT + 单方案重试 + sms 合并 + 删除完整性 + 越权 404）
- [ ] `contracts` lint 通过；`check-sync.mjs` openapi ↔ core 端点表一致
- [ ] `pnpm --filter @zsm/mobile typecheck && export` 通过（Metro 打包验证）

## B. 功能对齐（对照原型 18 页 + 差距 G1-G7）

- [ ] 主闭环 7 页 + 首页双状态（新用户/回访）+ scene + profile-setup（G1）
- [ ] plans 三方案轮播+对比+生成轮询；plan 详情+发型师参考卡（G2）
- [ ] checklist 乐观勾选；feedback 成功态个性化文案（G3）
- [ ] tools 4 页（hair 900ms 轮询/outfit/purchase/lab 占位）
- [ ] life 4 页（today/wardrobe/advisor/share）
- [ ] token 全部品牌值（G4）；分析文案服务端 stage 驱动（G7）

## C. 运行时（人工/真机，如实标注状态）

- [ ] 微信开发者工具打开 apps/miniapp 预览三 tab 与分包页跳转 —— 状态：
- [ ] 真机：登录、三图上传、扫描动效、方案对比滑块手感 —— 状态：
- [ ] Expo：iOS 模拟器/真机登录+主闭环走通 —— 状态：
- [ ] e2e 已覆盖的部分（契约行为）视为已验证

## D. 工程卫生

- [ ] `appearance-coach-prototype/` 零新增改动
- [ ] `git log --follow apps/server/cmd/api/main.go` 历史连续
- [ ] 分 WS 提交，mv 与 module 改名独立提交
- [ ] 媒体契约单测 4 断言在 CI 路径上
