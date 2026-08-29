# 首页核心卡片：Design QA

## Comparison target

- Source visual truth: `qa/wechat-ui-audit-2026-08-28/10-home-current.png` 与用户提供的紧凑卡片截图；用户确认的改造目标是保留原卡尺寸与视觉元素，将内容改为天气/季节/工作日驱动的今日造型穿搭。
- Implementation: 微信开发者工具 `jianwo-miniapp` 的 `pages/home/index`，当前卡片结构由 `miniapp/pages/home/index.wxml` 实现。
- Normalized comparison reference: `qa/wechat-ui-audit-2026-08-28/12-home-before-after-comparison.png`（用于确认首页整体层级；本轮卡片细节以用户提供的紧凑卡片为准）。
- Route/state: `pages/home/index`，已建立形象档案的回访用户。
- Simulator: WeChat DevTools, iPhone 12/13 (Pro), 67% canvas scale. Source image 658 × 1167 px including device frame; implementation source capture 1240 × 768 px including DevTools canvas. Content was cropped and normalized to 393 × 852 px for the comparison; canvas/DevTools chrome was excluded from the judgment.

## Comparison history

### Iteration 1 — prior state

Findings carried from the captured source:

- [P0] Custom profile control overlapped the Mini Program system capsule.
- [P0] `重新形象分析` was the strongest returning-user action, making the high-cost reset look like the recommended next step.
- [P1] Personalized recommendation was buried inside the archive card; quick tools, scenarios, and recent plan competed as three equal starting points.
- [P1] The first-screen comparison card began below the fixed tab bar, producing a clipped/unfinished impression.

### Iteration 2 — implemented state

Fixes made:

- Removed the redundant home profile control; the `我的` tab remains the stable profile destination.
- Replaced the returning archive card with a `今日顾问建议` task card and a single primary CTA: `生成我的日常方案`.
- Put scenario selection immediately after the suggested task; moved quick tools below the latest plan as a horizontal rail.
- Reduced the tab bar height and increased home scroll tail padding.

Post-fix evidence: `qa/wechat-ui-audit-2026-08-28/11-home-after-hierarchy.png` and the normalized comparison image above.

### Iteration 3 — contextual outfit card

User-directed changes:

- Replaced the report-like “今日顾问建议” copy with `上海 · 多云 · 24–30°C` and `周五 · 工作日`.
- Changed the greeting to `上午好，今日造型穿搭建议`.
- Added compact `发型 / 妆造 / 穿搭` rows while retaining the original card footprint, sage surface, right thumbnail, and moss CTA.
- Made the right thumbnail open an in-app full-screen lightbox. This avoids the DevTools-only indefinite loading state observed with local-file `wx.previewImage`.

Post-fix evidence: WeChat DevTools accessibility tree shows the updated greeting, weather/day context, three detail rows, `查看今日穿搭大图`, and the original independent scene section. Static mini-program validation and JavaScript checks pass.

## Fidelity surfaces

- **Fonts and typography:** Preserves the app’s PingFang-based UI typography; recommendation title, section headers, supporting copy and text buttons now have clear three-level hierarchy. No truncation is visible in the captured returning state.
- **Spacing and layout rhythm:** The advisory card is now the single dominant surface. Scenario cards use a consistently smaller radius and a lighter border. Fixed bottom navigation is shorter, and the page tail clears it.
- **Colors and tokens:** Uses existing warm ivory, moss green and low-contrast sage surfaces. Green denotes the one primary action rather than several unrelated states.
- **Image quality and assets:** Reuses existing app portrait and icon assets. The advice image has a consistent contained crop; no placeholder or fabricated visual asset was introduced.
- **Copy and content:** The card now provides a contextual outfit result rather than repeating report insights: weather/day context, hairstyle, makeup and outfit formula. The report remains a separate destination.

## Functional checks

- Static mini-program validation: `node miniapp/scripts/validate.mjs` passed for 14 pages.
- JavaScript syntax check: passed for every `miniapp/**/*.js` file.
- WeChat DevTools: loaded `jianwo-miniapp` at `pages/home/index`.
- Primary CTA test: `生成我的日常方案` successfully opened `pages/scene/index?scene=daily`, showing the four lightweight scenario questions and `生成日常方案` CTA.
- Clean recompilation returned no application error. The remaining DevTools console warnings are the gray-release base-library notice and two DevTools preload notices for `WAAutoService.js` / `WAServiceMainContext.js`; none point to application source.

## Residual P3 polish

- The captured DevTools canvas is scaled to 67%, so final optical type tuning should be rechecked on a physical device or 100% simulator scale.
- The weather values and city are currently demo data; production should source them from the user’s selected city and a weather provider, with a no-location fallback.
- The card’s lightbox is visually implemented and should receive one physical-device pass for image scaling and gesture dismissal.

## Final result

final result: passed
