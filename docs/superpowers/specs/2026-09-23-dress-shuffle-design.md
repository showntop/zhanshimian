# 换装洗牌接入小程序 · 双动画随机方案

日期：2026-09-23
分支：rebuild/daily-content
状态：已批准（双方案随机保留 / 两阶段映射 / mask 退役 / 滤镜换色）

## 背景与决策

首页每日内容的等待/揭晓动画现状（`daily-motion/`）：等待期 = Canvas 草图巡游
（`sketch.tsx`，简笔小人画分类草图），揭晓期 = CDN 序列帧（`frames.tsx`，
17 帧线稿→成形→定格）。整套按服务端下发脚本播放（kind 注册表），零素材、
三端稳定，但视觉偏安静。

`docs/prototypes/daily-daily-dressup-asset.html`（换装洗牌原型）已完成真实验收：
彩色 croquis 四维度换装（造型 8 / 配色 12 / 比例 3 / 发型 4，随机子集洗牌）、
飞卡上身、盖章揭晓。质感与爽感显著优于现状，但依赖 WAAPI/CSS filter，
且素材 12 张键出 PNG。

四项决策：

- **双方案随机保留**：两套动画都是一等公民。服务端按 `hash(userID+date)`
  稳定随机选 variant（`sketch` | `dress`），同一天同一人只看同一套；
  客户端按 stage kind 注册分发，天然支持两套共存。
- **两阶段映射**：dress 方案复用现有两阶段骨架——等待期 = 换装洗牌·循环轮
  （每轮随机子集，无限循环）；揭晓期 = 换装洗牌·收敛轮（generate 返回后
  加速锁定 target 四元组 + 飞卡连击 + 盖章）→ 海报。
- **滤镜换色，mask 退役**：12 色中 8 色走 CSS filter（基准砖红派生，
  数值经矩阵优化拟合）；4 深色走专属生图 + 白→alpha 键出（brightness<1
  会把白背景压灰，透明底不受影响）。
- **旧动画全程降级保留**：sketch 巡游 = 换装素材加载期的等待填充；frames
  帧揭晓 = dress 组件不可用时的揭晓降级；「不认识的 kind 一律回落」纪律
  不变。

## §1 协议扩展（服务端）

generate 的 settle 脚本按 variant 分支：

```jsonc
// variant = "dress" 时（新增 kind）：
{ "version": 2, "stages": [{
    "phase": "settle", "kind": "dress_lock",
    "params": {
      "target": { "look": "outfit", "color": 1, "waist": 0, "hair": 0 },
      "pace": "normal",            // normal | slow（等待久→收敛更隆重）
      "assets": { "base": "<CDN base>", "version": "20260923" }
    }
}]}
// variant = "sketch" 时：现状不变（sketch_tour + frames）
```

- **target 派生**：`look` ← content.category 映射（outfit/ratio/fit/
  occasion 四类就近归类，语义真）；`color/waist/hair` ← `hash(userID+date)`
  稳定随机（剧场真）。
- **prepare 不动**：洗牌循环是纯客户端行为，不需要巡游脚本；换装素材清单
  走客户端配置（版本号进 dress_lock.assets.version，预热缓存用）。
- **variant 选择**：`hash % 2`，服务端配置开关可覆盖（灰度/实验/紧急关停
  dress 回 sketch）。
- **旧客户端兼容**：旧版本不认识 dress_lock → 现有回落纪律（静态形态 +
  立即揭晓），不会空白。故 variant 对旧客户端安全，无需版本门槛。

## §2 客户端架构

新组件 `apps/miniapp/src/components/dress-shuffle/`，两阶段一体：

```
dress-shuffle/
  index.tsx     播放器：循环轮（waiting）/ 收敛轮（settling）状态机
  shuffle.tsx   洗牌引擎：候选池抽取、快切、飞卡（从原型移植）
  presets.ts    色板滤镜表、维度配置（池大小/候选库/target 映射）
  index.scss    壁龛/拱门/腰线/卡片/盖章样式（rpx 化）
```

渲染层映射（原型 → 小程序）：

| 原型机制 | 小程序实现 | 风险 |
|---|---|---|
| 快切闪换（换 src） | `<Image src>` 切换（原生） | 无 |
| 飞卡位移旋转 | WXSS transform + transition | 低 |
| 洗牌运动模糊 | filter: blur（快切段） | 中：端差降级为无模糊 |
| 换色滤镜 | WXSS filter: hue-rotate/saturate/brightness | **高：M1 真机验证** |
| 定格盖章 | opacity/scale keyframes（现有 stamp 模式） | 低 |

`filter` 不支持的端：换色维度退化为锁定基准砖红（洗牌/飞卡照常），
运行时 `Taro.getSystemInfo` 探测不可靠，采用**渲染探测**：置一个隐藏
filter 元素，`createSelectorQuery` 量测宽高是否变化（hue-rotate 不改变
几何 → 用 saturate(0) 的灰度对判别），失败即置能力位。

## §3 素材流水线

源图：`images/color/*.png|jpg`（2400×1792，基准砖红）。

```
裁切：按 look focus 预裁显示竖条（1008×1680 = 336×560css × 3dpr）
键出：白→alpha（灰度 min 215–240 线性）——源图已是键出版则跳过
量化：pngquant（插画扁平色，视觉无损）
产出：assets/color/*.png（目标 ≤350KB/张，8 张 ≈ 2.5MB）+ card-*.jpg 缩略卡（已有）
```

预载策略：首屏只预载 4 张 look 图（`Taro.getImageInfo` 串行 + 3s 超时
放行）；发型 4 张在造型锁定后懒加载（发型维度排最后，来得及）；
卡片缩略图随 look 图附带。预载失败/超时 → 回落 sketch 巡游。

## §4 降级矩阵

| 情况 | 行为 |
|---|---|
| variant=sketch | 现状全流程（巡游 + 帧揭晓） |
| variant=dress，素材预载失败/超时 | sketch 巡游顶等待 + frames 揭晓 |
| variant=dress，部分发型图缺失 | 发型维度不进池（洗牌照常） |
| variant=dress，端不支持 filter | 换色维度锁定基准砖红 |
| 不认识 dress_lock（旧客户端） | 静态形态 + 立即揭晓（现有纪律） |
| 减动效 | 洗牌塌缩为四张 target 卡直出 + 盖章 |

## §5 分期

- **M1 滤镜真机验证**：dress-shuffle 骨架内置渲染探测 + 真机三端检查
  （iOS/Android/Skyline）。
- **M2 组件移植**：循环轮（洗牌引擎 + 随机子集）→ 收敛轮（加速锁定 +
  飞卡连击 + 盖章）→ onSettled。
- **M3 服务端脚本**：variant 哈希 + dress_lock 下发 + 降级矩阵联调。
- **M4 素材流水线**：裁切/键出/量化脚本进仓库，CDN 上传，预载接线。
- **M5 真机打磨**：节奏（等待轮/收敛轮速度比）、内存（图片释放）、
  减动效。

## §6 验收

- 同一用户同一天 variant 稳定；两天分别见到两套动画
- dress 全流程：洗牌循环 → generate → 收敛轮锁定 target → 盖章 → 海报，
  与原型观感一致
- 三条降级路径逐条演练不空白、不卡住
- 减动效开启时可用
