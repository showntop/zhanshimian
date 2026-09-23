# 换装洗牌接入小程序 · 实施计划

日期：2026-09-23
设计：`docs/superpowers/specs/2026-09-23-dress-shuffle-design.md`
原型：`docs/prototypes/daily-dressup-asset.html`（已验收的真源）

## M1 · 滤镜真机验证（前置风险清零）

- [x] 配置覆盖位 + 降级链路：storage `zsm_dress_filter_off=1` 模拟不支持滤镜
      → `colorLocked` 全链路生效（人物锁基准砖红、洗牌/飞卡照常、收敛 target
      钳制回砖红、chips 语义一致）
- [ ] 渲染探测：几何探测方案存疑——`createSelectorQuery` 返回布局矩形，
      filter 不改变几何，saturate(0) 灰度对判别测不出能力位。待真机确认
      skyline/webview 各端的 filter 实测后定方案（降级开关已就位，探测只是
      自动化入口）
- [ ] 微信开发者工具 iOS/Android/Skyline 三端检查 hue-rotate 组合滤镜
      （另注：`.ds__figure` 的 mix-blend-mode: multiply 一并验）

## M2 · dress-shuffle 组件（本计划主体）

- [x] `presets.ts`：LOOKS(8)/COLORS(12+filter)/WAISTS/HAIRS 库、pickPool、
      buildRunDims（随机子集 + target 保底 + 维度乱序 + force/order 钩子 +
      hairAvailable 出池位）；另补 stableHash/stableVariant/stableTarget
      （M3 就位前的客户端稳定派生，纯函数可单测）
- [x] `index.tsx` 状态机：waiting=循环轮（每轮随机子集洗 3~4 维，轮间
      无缝循环）/ settling=收敛轮（加速锁定：每维快切→飞卡→上身，
      4 维连击 + 盖章）→ onSettled；修复 PoolState.target 类型、
      发型卡命名（card-outfit-{key}-v2.jpg，与飞卡/原型一致）
- [x] 减动效：塌缩为四张 target 卡直出 + 盖章；SCSS 侧
      prefers-reduced-motion 全部归零（AGENTS 动效规约）
- [x] 素材预载 hook（`use-dress-assets.ts`）：核心 4 张串行 getImageInfo +
      3s 总闸（齐了提前放行，缺/超时 failed → 父级回落 sketch）；其余
      look 图 + 卡片预热；发型人物图 + 发型卡懒加载 → hairReady 位
- [x] `index.scss`：原型 CSS px → rpx（750 设计宽 1:1）移植 + 降级样式
- [x] 首页接线：`pages/home` 按 variant 分发——dress 素材就绪播洗牌
      （waiting 循环轮 / settling 收敛轮 + onSettled→reveal），否则原
      sketch 巡游 + 帧揭晓全流程顶位；storage 可覆盖 base / 强制滤镜关闭

## M3 · 服务端脚本

- [x] `presentation.go`/`variant.go`：`hash(uid+date) % 2` 选 variant，
      settle 脚本下发 `dress_lock`（target：look←category 就近归类，
      color/waist/hair←稳定随机，语义值经共享词汇表对接客户端）；
      `pace` 按耗时定档（≥15s → slow，与 converge 的节奏哲学一致）；
      `assets.base`（PUBLIC_BASE_URL）+ version 位下发；assetBase 为空时
      dress 不启用（素材必失败，不空转）。客户端已删本地 stableTarget
      派生：等待期本地预测、收敛期按脚本 kind 分发，词汇表对不上回落旧线
- [x] 配置开关：`DAILY_MOTION_VARIANT`（空=自然分流 / sketch=关停回旧线 /
      dress=全量放量），config 校验合法值，bootstrap 接线
- [ ] prepare 不动（已遵守）；降级矩阵逐条联调（待真机 + CDN）

## M4 · 素材流水线

- [x] 流水线脚本进仓库：`apps/miniapp/scripts/dress-assets.py`（PIL）——
      键出（白→alpha 215–240 线性 + un-blend 防暗边）→ 降采样 1500×1120
      （显示区 2x）→ FASTOCTREE 256 色量化；卡片缩略图取原型已验收产物直通。
      实测人物图 52–80KB/张（预算 ≤350KB），深底抽检无白边暗边、线稿完整
- [x] 落位与接线：产物进 `apps/server/assets/daily/dress/color/`（与序列帧
      揭晓同一 `/assets/` 静态路由，免 CDN 上传步骤）；服务端 dress_lock
      下发 `assets.base = PUBLIC_BASE_URL + /assets/daily/dress`，等待期
      客户端预测同址派生（storage 可覆盖本地联调另一台源）。本地起服
      冒烟 HTTP 200/55KB；生产 404 待部署（部署前预载必失败 → 自动回落
      sketch，行为安全）
- [x] 预载策略：首屏核心 4 look（3s 闸），其余 look + 卡片预热，发型懒加载
      （use-dress-assets.ts 已实现）

## M5 · 真机打磨

- [ ] 等待轮/收敛轮速度比、洗牌 blur 端差、内存释放、减动效终验

## 验收

- 同一用户同一天 variant 稳定，两天见两套
- dress 全流程与原型观感一致（快切/飞卡/盖章/海报滑入）
- §4 降级矩阵逐条演练不空白不卡住
