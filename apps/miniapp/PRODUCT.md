# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Taro 4 + React + TypeScript + Vite，编译到微信小程序（weapp）。设计 token 单源在 `packages/design`（tokens.ts → tokens.wxss）；文案与契约单源在 `packages/core`。构建：`pnpm dev:weapp`（微信开发者工具打开本目录）；`pnpm typecheck` / `make check` 校验。

## Users

主要用户两类并重：
1. **场合驱动型**：面试、婚礼、约会、聚会等具体场合前，需要一套能直接照着准备的发型/妆容/穿搭的人（场景 tab + 场合 Brief 线）。
2. **日常形象管理型**：想持续优化自己形象、周期性回访的用户（今日穿搭 / 每日任务线）。

## Product Purpose

uplook 是懂发型、妆容和穿搭的 **AI 形象顾问**：用户拍摄建档 → 形象分析报告 → 基于报告生成三套可执行的造型方案（卡堆决策：喜欢/跳过）→ 效果图预览。成功 = 留存回访（每日建议线把用户拉回来）。

## Positioning

每日建议 + 建议真实试装 + 效果预览（基于用户本人照片的 AI 效果图），分析报告是这一切的基础。相邻产品只能给泛泛的风格建议；uplook 的机制是「读你的报告 → 出可执行步骤 → 用你本人的脸预览效果」三段闭环。

## Operating Context

- 移动端微信生态内使用；拍照上传是核心输入动作。
- 每轮方案生成需 1-2 分钟（异步任务轮询模型），等待体验是产品的一部分。
- 用户在「生成中 ↔ 决策中 ↔ 结算/往期」多状态间往返，历史轮次可回看。

## Capabilities and Constraints

- 功能域：拍摄建档（capture）、形象分析（assessment/report）、方案决策（planning：卡堆 + 场景 Brief + 往期历史）、执行清单（execution/checklist）、反馈（feedback）、衣橱（wardrobe）、今日/每日（daily/today）。
- **数据真实性红线**：API 失败/为空必须渲染空态/错误态，绝不静默回退内置内容；内置模特图只能经 `exampleImage()` 且必须挂角标弱化。
- **AI 标识红线**：AI 生成图必须显式标注（《人工智能生成合成内容标识办法》）。
- **契约优先**：接口改动先改 `contracts/openapi.yaml`；`/v1` 内只加字段。
- 异步任务统一模型：创建返回 `202 + task`，轮询 `GET /v1/tasks/{id}`。
- 质量门禁会拒绝低质量生成（plan/render 双次拒即失败），失败态必须可见、可懂、有出路。

## Brand Commitments

- 名称 uplook，slogan「今天最好看」。
- 视觉语言：苔绿（moss）× 墨色（ink）的裁缝/纸样车间隐喻（纸样台进度动画、宋体数字等），详见 `apps/miniapp/src/features/planning/index.scss` 注释与 `packages/design`。
- 中文界面；文案单源 `packages/core/src/copy/zh.ts`。

## Evidence on Hand

- 形象分析报告（真实用户数据）。
- 方案集历史与用户喜欢/跳过决策数据（plan_sets / plan_variant_decisions）。
- 质量评估记录（quality_evaluations：decision + reason_codes）。
- 原型参考（只读）：`appearance-coach-prototype/`。

## Product Principles

1. **真实优先**：不虚构用户没给的内容；空态/失败态与成功态同等对待。
2. **可执行**：每条建议都该能照着做（步骤、部位、避免什么），不是形容词。
3. **本人视角**：效果图和方案都围绕「你」，不用泛模特替代。
4. **等待也是体验**：1-2 分钟生成期给进度、给旧成果可看，不让人干等。
5. **历史是资产**：每次生成、每轮决策都留得下、回得去。

## Accessibility & Inclusion

- 微信小程序可访问性基线：可点热区 ≥44px、文字对比度 4.5:1。
- 中文阅读节奏：正文 ≥24rpx，层级靠字重与墨色深浅区分。
