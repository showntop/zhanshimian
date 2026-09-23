# 每日动画 · 真人素材制作 SOP

> 产出物是**视频**，由工程侧抽帧成 JPEG 序列帧后接入 `daily-motion` 播放器。
> 用途只一个：首页每日卡片的等待（巡游）与揭晓（收敛）动画。**今日页不做。**

## 一、素材怎么被消费

| 环节 | 用哪段素材 | 播放方式 |
|---|---|---|
| **巡游 roam** | 视频 A 全段 | 循环播，每帧停留约 400ms，一轮约 10s。首尾都是残影态 → 接缝天然无缝，不需要 cross-fade |
| **收敛 settle** | 视频 B 全段 | 播一次，逐帧拉长停留（90ms → 330ms），停在最后一帧 |
| **揭晓 reveal** | 不用素材 | 最后一帧 cross-fade 到真实海报（海报由内容数据渲染） |

关键点：**巡游要"一直在变、没有终点"，收敛要"变到某一套然后停住"**。这是两段视频分开做的唯一原因——一段视频同时满足两者会互相破坏。

## 二、规格

| 项 | 值 | 说明 |
|---|---|---|
| 比例 | **4:3 横版** | 卡片是 750×560rpx ≈ 1.34:1，4:3 几乎零裁切损失 |
| 分辨率 | **1080×810**（最低 1024×768） | 卡片物理像素 750×560，1080 留足裁切余量 |
| 时长 | A = 10s，B = 5s | 见上表 |
| 机位 | 固定，无运镜、无剪辑、无变焦 | 运镜会毁掉抽帧的连续性 |
| 主体 | 模特原地不动，只有服装在变 | 走位 / 转身会导致抽帧连播跳帧 |
| 抽帧后 | 750×560 JPEG，约 36 帧，总 ≤400KB，走 CDN | 不占主包（主包只剩 40KB） |

## 三、工具要求

支持以下三点的图生视频工具均可（可灵 / 即梦 / Runway / Veo 等）：

1. 图生视频（可上传首帧参考图）
2. 首尾帧控制
3. 单段可出 10s（或支持「延长 / 续写」）

## 四、已备好的依赖

| 文件 | 说明 |
|---|---|
| `docs/prototypes/assets/ref-roam-ghost.png` | **1080×808（4:3，1080×810 差 2px 可忽略）。残影态、无黑边、背景正确——直接用作视频 A 的首帧，也可直接当尾帧** |
| `docs/prototypes/assets/ref-style-storyboard.png` | 三格分镜示意（残影 → 过渡 → 定格），**只能当风格基准，不可当素材**：它是三格拼板、且上下各带 191px 黑边 |

## 五、步骤

### 步骤 1 · 生成巡游尾帧（可选）

**最省事的做法：跳过这一步**，视频 A 的首帧和尾帧都用 `ref-roam-ghost.png` —— 首尾完全一致，循环天然无缝。风险是 AI 可能生成得偏保守、变化幅度小；**先试，变化不够再来补这张**。

若要单独生成一张不同的残影态尾帧：

- **模型**：文生图
- **尺寸**：1080×810（4:3 横版）
- **风格参考**：上传 `ref-roam-ghost.png` 对齐人物与色调
- **输出**：`ref-ghost-b.png`

```
Editorial fashion photography, a striking East Asian woman fashion model, long straight dark hair, refined distinctive features, natural relaxed standing pose, seamless warm greige studio backdrop, soft diffused directional light, gentle floor reflection. Her outfit appears as multiple-exposure ghosting: several semi-transparent overlapping versions of her in the same spot wearing different garments — a long coat, a knit vest, wide-leg trousers — with soft motion trails, colors within warm brown, oatmeal and cocoa. Her face stays sharp and composed. 4:3 horizontal framing, model centered with empty background space on both sides, same scale and position as the reference image. No text, no watermark, no logos.
```

### 步骤 2 · 生成视频 A（巡游，10s）

- **模型**：图生视频
- **尺寸**：1080×810，**10s**
- **首帧**：`ref-roam-ghost.png`
- **尾帧**：`ref-ghost-b.png`；没做步骤 1 就再用 `ref-roam-ghost.png`
- **输出**：`segA.mp4`

```
Over ten seconds, single continuous take, fixed camera. The woman's outfit continuously transforms through many different looks at a steady even rhythm — long coat, blazer, cropped knit top, knit vest, midi skirt, wide-leg trousers, tonal belted layers — with soft multiple-exposure ghost trails. Her face, hair and posture remain sharp, still and perfectly constant throughout. She stays in exactly the same spot — no walking, no turning, no stepping, only the clothing changes. No camera movement, no cuts, no zoom. Editorial fashion film, seamless warm greige backdrop, soft diffused light, warm neutral palette. No text, no watermark, no logos.
```

> 若工具单次上限 5s：先用上面这段出 5s，再延长 5s，延长词：
> `Continue: the outfit keeps transforming through further looks at the same steady rhythm, more variations — knit vest, wide-leg trousers, belted coat, tonal layers. Same woman, same pose, same framing, same lighting. Fixed camera, no movement, no cuts.`

### 步骤 3 · 生成收敛尾帧（干净定格图）

- **模型**：文生图
- **尺寸**：1080×810（4:3 横版）
- **风格参考**：上传 `ref-roam-ghost.png` 对齐人物与色调
- **输出**：`ref-roam-final.png`

```
Editorial fashion photography, a striking East Asian woman fashion model, long straight dark hair, refined distinctive features, natural relaxed standing pose, hands at her sides, seamless warm greige studio backdrop, soft diffused directional light, gentle floor reflection. Wearing a cropped knit top in warm taupe over high-waisted wide-leg trousers in oatmeal, a thin belt, pointed heels. Single crisp figure — absolutely no ghosting, no motion blur, no double exposure — real fabric texture with visible drape. Premium, calm. 4:3 horizontal framing, model centered with empty background space on both sides, same scale and position as the reference image. No text, no watermark, no logos.
```

### 步骤 4 · 生成视频 B（收敛，5s）

先导出 A 的最后一帧做首帧：

```bash
ffmpeg -sseof -0.1 -i segA.mp4 -frames:v 1 -q:v 2 segA_last.jpg
```

- **模型**：图生视频
- **尺寸**：1080×810，**5s**
- **首帧**：`segA_last.jpg`
- **尾帧**：`ref-roam-final.png`
- **输出**：`segB.mp4`

```
The transformations progressively decelerate: fewer changes, slower rhythm, the multiple-exposure ghost trails thin out and gradually collapse into one single crisp clean figure wearing a cropped knit top over high-waisted wide-leg trousers with a thin belt, coming to a complete stop. Her face, hair and posture stay sharp and constant. Fixed camera, no camera movement, no cuts, no zoom. No text, no watermark, no logos.
```

## 六、验收（每段生成后立刻自查，不过就重跑）

- [ ] **脸没变形**（延长段最容易崩）
- [ ] **模特没走位、没转身**（位移会导致抽帧连播跳帧）
- [ ] **背景没跳变**（延长时背景突然变亮/变暗最常见）
- [ ] 无文字、无水印、无 logo
- [ ] 视频 B 的最后一帧确实是 crop top + 高腰阔腿那套
- [ ] 视频 A 全程一直在变换（没有中途静止几秒）

## 七、落盘

全部放 `docs/prototypes/assets/`：

```
ref-roam-ghost.png       首帧 + 风格基准（已有）
ref-ghost-b.png          巡游尾帧（步骤 1，可选）
ref-roam-final.png       收敛尾帧（步骤 3）
segA.mp4                 巡游 10s（步骤 2）
segA_last.jpg            A 的末帧（步骤 4 导出）
segB.mp4                 收敛 5s（步骤 4）
ref-style-storyboard.png 风格参考，三格分镜（不可当素材）
```

## 八、工程侧后续（素材到位后我做）

```bash
# 拼接（若分了两段）
ffmpeg -f concat -i list.txt -c copy roam-outfit.mp4

# 抽帧：12fps → 挑帧 → 裁 750×560 → JPEG 量化
# 巡游段 ~24 帧（循环用）＋ 收敛段 ~12 帧
# 目标：单帧 <15KB，总计 ≤400KB
```

然后接入 `daily-motion`：巡游按分类轮播（每分类一 stage，叠加层由前端渲染器画），收敛按 `interval [90,330]` 逐帧拉长、定格后 cross-fade 到真实海报。

## 九、补充素材：面料 / 技巧

巡游要做分类轮播，其中六个分类复用 `segA` 素材 + 代码叠加层即可，只有这两个做不出来，需要单独素材。

**只要 5s**——巡游每个分类停留 3s，不需要像 segA 那样 15s。

### 面料 fabric

| 项 | 值 |
|---|---|
| 用途 | 巡游「在看面料」段的底图 |
| 类型 | 视频 5s（备选：3 张静态特写，我做 cross-fade） |
| 尺寸 | 1080×810（4:3），固定机位 |
| 风格参考 | 上传 `ref-roam-ghost.png` 对齐光线与色调 |
| 输出 | `seg-fabric.mp4` |

```
Extreme close-up of fabric on a garment, filling the entire frame — visible weave texture, drape and soft sheen. Over five seconds the fabric itself changes: heavy matte wool, then crisp cotton poplin, then fluid satin, then fine knit — weave, weight and light reflection visibly transforming while the framing stays fixed. Warm neutral palette (oatmeal, taupe, cocoa, sage), soft diffused studio light with a slow highlight moving across the surface. No human, no face, no hands, no text, no watermark. Fixed camera, no cuts. 4:3 horizontal framing.
```

### 技巧 howto

| 项 | 值 |
|---|---|
| 用途 | 巡游「在看穿法」段的底图 |
| 类型 | 视频 5s（备选：3 张动作关键帧） |
| 尺寸 | 1080×810（4:3），固定机位 |
| 输出 | `seg-howto.mp4` |
| 风险 | **AI 手部最容易变形**，崩了就退回静态关键帧 |

```
Close-up of hands performing a styling action on clothing — rolling up a shirtsleeve neatly, then tucking a hem into a waistband, then knotting a belt — hands move slowly and clearly, the fabric responding. Warm greige seamless backdrop, soft diffused light, warm neutral palette (oatmeal, taupe, cocoa). Hands and garment only, no face, no head. Realistic hands with correct anatomy. No text, no watermark. Fixed camera, no cuts. 4:3 horizontal framing.
```

### 两者的静态备选（视频崩了用这个）

面料，三张不同材质：

```
Extreme close-up of matte wool fabric on a garment, filling the frame, visible weave and soft drape, warm oatmeal tone, soft diffused studio light. No human, no text, no watermark. 4:3 horizontal framing.
```
（把 `matte wool` 依次换成 `crisp cotton poplin`、`fluid satin`）

技巧，三张动作关键帧：

```
Close-up of a shirtsleeve being rolled up, starting state unrolled and smooth — hands and garment only, warm greige backdrop, soft light, realistic hands with correct anatomy, no face, no text. 4:3 horizontal framing.
```
（后半句依次换成 `rolled halfway, a fold forming`、`neatly rolled above the elbow, crisp fold`）

## 十、线稿素材（等待方式二：线稿逐层补全）

等待期的另一种表现：线稿 → 比例线 → 上色 → 细节 → 定格。**颜色与服装可由当天内容驱动**，等待动画画的就是今天要讲的那身。

### 硬约束（不满足会导致上色区域提取失败）

| 项 | 要求 | 原因 |
|---|---|---|
| 纯黑白 | 纯白底 `#FFFFFF` + 纯黑/深墨线条，**无灰度、无阴影、无排线、无渐变** | 要二值化 + flood fill |
| **轮廓闭合** | 每个区域的轮廓线必须封闭，**无断笔、无飞白、无速写感** | 断线会让填充漏到背景（之前手部漏到 65% 面积） |
| 线宽 | ≥2px（按 1024 宽算） | 太细二值化后断线 |
| 脸 | 五官清晰（眉 / 眼 / 鼻 / 唇），美女 | 上色后是美妆插画 |
| 构图 | 正视图、全身、站姿自然、居中 | 要展示比例与搭配 |
| 其它 | 无文字、无水印、无签名 | — |

### 规格

**1080×810（4:3 横版）** · PNG · 白底 · 命名 `lineart-<分类>.png` · 放 `docs/prototypes/assets/lineart/`

卡片是 750×560rpx ≈ **1.34:1 横版**，所以线稿必须出横版——竖版 3:4 放进去两侧会空一大块。人物占满画面高度、站在左侧三分之一，右侧留白给文案。

### 通用前缀（每张都带）

```
Fashion illustration line art, a beautiful East Asian woman, refined delicate facial features with eyes, eyebrows, nose and lips clearly drawn, long dark hair, elegant natural standing pose. Clean continuous black ink line drawing on a pure white background, every contour fully closed with no gaps, no broken lines, no sketchy strokes, no shading, no hatching, no gradient, no fill, no grey tones — pure black lines on pure white only. Full body front view, the figure occupying the full height of the frame and positioned in the left third, leaving generous empty white space on the right. Flat vector-like quality. No text, no watermark, no signature. Horizontal 4:3 framing.
```

### 各分类

| 分类 | 服装描述（接在通用前缀后） |
|---|---|
| 搭配 `outfit` | `wearing a cropped fine-knit sweater over high-waisted wide-leg trousers with a thin belt and pointed heels` |
| 比例 `ratio` | `wearing a short fitted top ending above the waist over high-waisted long straight trousers, minimal simple shoes, the waistline clearly visible and emphasized` |
| 版型 `fit` | `wearing a fit-and-flare A-line midi dress with a defined waist and a wide flared skirt, simple heels, the silhouette clearly readable` |
| 场合 `occasion` | `wearing a crisp button-up shirt tucked into tailored straight trousers with a long unstructured blazer over it, low heels, smart office look` |
| 技巧 `howto` | `wearing a relaxed linen shirt with the sleeves rolled up to the forearm, tucked into straight trousers, the rolled sleeve detail clearly drawn` |

先出 **搭配 / 比例 / 版型 / 场合** 四张，技巧最后补。

### 工程侧（素材到位后我做）

```bash
# 每张线稿自动提取上色区域 mask（flood fill，从内部种子点）
python3 scripts/lineart-masks.py assets/lineart/lineart-outfit.png
# 输出 face / neck / hair / arms / hands / top / bottom / belt / lips 等 mask
# 面积异常（<0.05% 或 >40%）即判定漏填充，回退几何近似并告警
```

## 十一、工程落位（2026-09-21 已接线）

最终方案按 `docs/prototypes/daily-combo-sim.html` 定稿：**草图巡游（等待）→ 序列帧（揭晓）**。

| 环节 | 实现 | 素材 |
|---|---|---|
| 巡游 roam | `kind=sketch_tour`，客户端 Canvas 逐笔画出（`daily-motion/sketch.tsx`） | 零素材 |
| 揭晓 settle | `kind=frames`，逐帧播完定格再上海报（`daily-motion/frames.tsx`） | `assets/daily/reveal/f_01..17.jpg`（reveal.mp4 抽帧，760×570 JPEG，共 208KB） |

- 帧放服务端 `assets/daily/reveal/`，走既有 `/assets/` 静态路由；服务端用 `PUBLIC_BASE_URL` 拼 URL 随 settle 脚本下发（`params.urls + interval_ms + hold_ms`）。
- **换素材**：新帧覆盖该目录（文件名不变）即可，脚本与服务端都不用改；帧数变了才需要改 `presentation.go` 的 `revealFramesCount`。
- 服务端未配 `PUBLIC_BASE_URL`（本地裁剪）时 settle 退回 CSS 形态收敛（旧 converge 脚本），不至于直接跳海报。
- 上面的 segA/segB 真人素材路线保留为后续升级：巡游换成真人残影帧、揭晓换成分类定格，协议不变、只换 kind 与素材。
