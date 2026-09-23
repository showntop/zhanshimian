# 等待动画「换装洗牌」· 人物素材规范

> 用途：首页每日建议生成等待动画左侧人物。原型见 `docs/prototypes/daily-dressup-asset.html`
> （动画逻辑）/ `daily-dressup.html`（SVG animatic，分镜规范）。
> 展示环境：小程序首页过场，人物展示高约 500 逻辑像素（rpx 750 宽体系），
> 缩略卡 104×124。**小尺寸展示，细节不可见，重点是比例、姿态、大轮廓。**
> 交付对象：图像生成管线（`provider/ai/image.go` 链路 + 人工筛图）。

与《每日内容·素材需求清单》「不出现人」约束的关系：**本规范是受控例外**。
画的是非写实时尚插画 croquis（夸张比例、无面部细节），不是真人照片；等待
动画是装饰性过场，人物不指向用户本人，不构成「你会穿成这样」的产品暗示。
若后续合规口径变化，此例外需重新评审。

## 架构（2026-09-23 定版）：彩色图 + 滤镜换色

**每个 look 一张基准色（砖红）彩色图，其余色板颜色用 CSS filter 运行时派生。**
人物是单色线稿风（线/发深棕、白底、无五官），服装是全图唯一彩色区——
`hue-rotate/saturate/brightness` 几乎只动服装，快切闪染保留、零额外下载。

> 曾经的「线稿 + mask 染色」方案（2026-09-22）退役：mask 与线稿分属两个
> 坐标系（整图压扁 vs cover 裁切）导致对齐 bug，且平涂色在真实浏览器里
> 均匀度/质感不到位。差分脚本存档于 `assets/masks/make_masks.py`，
> 线稿版 look 图仍归档于 `assets/lineart/`（彩色图的生成中间件）。

## 一、交付格式与命名

| 素材 | 路径 | 数量 | 说明 |
|---|---|---|---|
| 造型彩色图 | `docs/prototypes/assets/color/{look}.jpg` | 4 | look ∈ `outfit`(针织+阔腿裤) / `ratio`(短上衣+高腰裤) / `fit`(A字连衣裙) / `occasion`(西装叠穿)；基准色=砖红 |
| 发型彩色图 | `docs/prototypes/assets/color/outfit-{style}.jpg` | 4 | style ∈ `wave`(大波浪) / `bun`(丸子头) / `bob`(齐耳短发) / `long`(长直发)；发型维度在造型锁定后，只做 hero look × 4 |

- 画布：**4:3 · ≥2048×1536**（模型原生 2400×1792 即用，不再缩放；后续
  编辑全部跟随底图尺寸）
- 格式：**透明底 PNG（白→alpha 键出，键出阈值：灰度 min 215–240 线性）**
  ——透明底让配色滤镜（含 brightness<1 的深色）不影响背景，multiply
  依赖一并退役。生产端体积策略（缩放/按需加载）进小程序任务再定
- 全部素材**同一基准色**：砖红 `#B4553C` 系；其余 5 色运行时滤镜派生（§四）

## 二、人物设定（croquis bible）——所有素材同一人

任何一张偏离这组设定即返工：

- **比例**：8.5–9 头身；肩宽 ≤ 1.5 头宽；腿长占全身 55% 以上
- **姿态**：S 站姿。重心在左腿，右腿放松微交叠；单手叉腰（肘外展、手落在
  胯上，**画面左侧、与底图同侧**）；另一臂垂放微弯；穿细高跟（**必须有
  鞋**）；头微侧（约 7°），下巴微抬
- **发型（造型步默认态）**：中分长直发——发型是第四维度的变量，造型步
  不许漂移
- **线条**：单色深棕黑（#3B352B 等效），均匀粗细，连续长弧线，无网点
- **面部**：极简或无五官（发丝+姿态承担表达，避开「像谁」）
- **构图**：人物完整（头顶与脚跟各留 ≥8% 边距），整体居中——四张叠加时
  头/手/脚必须重合

## 三、出图步骤（runbook，全部已完成 ☑）

**模型**：Nano Banana Pro（Gemini 3 Pro Image）；备选 GPT Image 2 整链切换。
参考图 = 姿态锚（v1–v4 漏参考图导致姿态/鞋/叉腰侧漂移的教训）。
星形分支：每步的编辑输入都是同一条母版图，不链式。

| # | 步骤 | 输入 | 生成→交付 | 交付文件 | 状态 |
|---|---|---|---|---|---|
| 0 | 线稿基准 croquis | 步骤 0 prompt（紧身衣） | 8 → 1 | `images/lineart/_base_x.jpg/ok-…jpg` | ☑ |
| 1 | outfit 线稿 | _base（参考图）+ 服装句 | 8 → 1 | `images/lineart/lineart-outfit-v5/…lmleo4yxri.jpg` | ☑ |
| 2 | **上色 outfit（彩色母版）** | 步骤 1 线稿 + 上色 prompt | 2 → 1 | `images/color/outfit.png` → 转正 `assets/color/outfit.jpg` | ☑ |
| 3 | ratio / fit / occasion 彩色版 | **步骤 2 产物**（星形） | 各 2 → 1 | `assets/color/{ratio,fit,occasion}.jpg` | ☑ |
| 4 | 发型 ×4 彩色版 | **步骤 2 产物**（星形） | 各 2 → 1 | `assets/color/outfit-{wave,bun,bob,long}.jpg` | ☑ |
| 5 | 白→alpha 键出 | 步骤 2–4 全部成图 | — → 12 | `assets/color/*.png`（透明底） | ☑ |
| 6 | 验收打包 | §五 全项 | — | 归档 | ☑ |

合计 **8 张彩色交付**（4 look + 4 发型）。加色 = 改滤镜预设（0 张图）；
加 look / 加发型 = 各 1 张编辑。

### 各步骤提示词（实际执行版）

**步骤 0 · 线稿基准**（紧身衣，白底纯线稿）：

```
Full-body fashion illustration lineart of an elegant female croquis, elongated
9-head proportions, sinuous S-pose with weight on the left leg, right hand
resting on hip, left arm relaxed and slightly bent, wearing a fitted bodysuit
and high heels, head tilted about 7 degrees with chin slightly lifted, no
facial features, minimal clean continuous line of uniform weight, no shading,
editorial style, full figure centered with generous margins, pure white
background, black-brown ink lines --ar 4:3
```

**步骤 2 · 上色（核心步骤；输入 = 线稿 outfit）**：

```
Edit this fashion lineart illustration: fill the garment — the loose
dropped-shoulder top and the high-waisted wide trousers — with a solid warm
brick red, with subtle fabric shading inside the colored area only. Keep the
exact same pose, outlines, long dark-brown hair, stiletto heels and pure
white background, in the same elegant editorial illustration style. Only the
garment gains color; hair, lines, shoes and background stay unchanged. Change
nothing else.
```

**步骤 3 · 彩色换装（输入 = 上色 outfit；只换服装句）**：

```
Edit this fashion illustration: replace the outfit — the loose
dropped-shoulder top and the high-waisted wide trousers — with {新服装句}.
The new garment keeps the same brick red with subtle fabric shading. Keep the
exact same pose, outlines, long dark-brown hair, stiletto heels and pure white
background, in the same editorial illustration style. Change nothing else.
```

- ratio：`a cropped short top and straight high-rise trousers, showing a sliver of waist`
- fit：`an A-line midi dress with a defined waist seam`
- occasion：`a single-breasted structured blazer with open lapels over a simple top, paired with straight trousers`

**步骤 4 · 彩色换发（输入 = 上色 outfit；只换发型句）**：

```
Edit this fashion illustration: replace only the hairstyle with {发型句}.
Keep the brick-red garment, pose, outlines, stiletto heels and pure white
background exactly unchanged. Change nothing else.
```

- wave：`long glamorous waves falling past the shoulders to mid-back`
- bun：`a high topknot bun with a clean exposed neck`
- bob：`a sharp chin-length bob tucked behind one ear`
- long：`long straight hair parted slightly off-center, falling to mid-back`

### 教训合集（2026-09-22/23 实测，出图前先读）

1. **材质词（knit 等）+ 只靠负面词** = 灰米背景 + 排线填充 → 线稿约束写
   正面指令，服装句只用轮廓词（彩色架构下此条弱化，上色步反而要求 shading）
2. **lookbook「正面/背面视图对」** 是强先验 → prompt 写 `exactly ONE single
   figure`；程序预筛数簇（全高连通域 == 1），视觉终审都容易漏
3. **姿态回退 / 叉腰侧镜像 / 鞋丢失 / 发型漂移**：重绘区域大时 keep 不够 →
   姿态、叉腰侧（画面左）、鞋、长发逐条显式重述
4. **参考图必须带**（v1–v4 漏了 = 姿态/鞋/侧向全漂）；编辑步只喂一张参考
   （上色版母版），多喂引入第二姿态源

### 程序化预筛（PIL，人眼前先跑，任一不过直接淘汰）

1. 白底：灰度中位数 >250、四角 >245（彩色图不看深色占比——衣服本身就是
   大色块）
2. 人数：全高连通域（>1500px 且高 >300，600×448 缩样）恰好 1 个
3. 锚点：头/脚质心 vs 母版 <1.5%（发型图的头部偏移可放宽——发型本来就
   在变）

### 眼筛三件套

**鞋还在**（高跟没变光脚）、**叉腰手在画面左侧**（家族同侧）、**只有该变
的地方变了**（造型步：发型不动；发型步：服装不动）。

## 四、运行时对照（工程侧）

| 维度（顺序） | 机制 |
|---|---|
| 1 造型 | 整图换 `assets/color/{look}.jpg`；快切段闪切=试衣 |
| 2 配色 | **CSS filter 派生**（见下表），快切段闪染 |
| 3 比例 | 腰线 overlay 46/52/58% 三档 |
| 4 发型 | 整图换 `assets/color/outfit-{style}.jpg` |

**配色滤镜预设**（基准=砖红）：人物是单色线稿风，`hue-rotate/saturate`
几乎只动服装。**亮度 <1 的深色不用滤镜**——brightness 会把白背景压灰
（multiply 下人物背后出现灰框），深色四张走专属生图（白→alpha 键出后
以带透明通道的裁切图入库，滤镜/混合对透明区无作用）：

| 色板 | 机制 |
|---|---|
| 砖红 #B4553C | 基准（母版原色，无滤镜） |
| 驼色 / 燕麦 / 浅灰 / 奶油白 / 粉棕 / 雾蓝 / 橄榄 | 滤镜（brightness ≥1，白点被钳位保持，背景安全） |
| 墨绿 / 藏蓝 / 酒红 / 炭灰 | **专属生图**（步骤 5；透明底裁切图） |

滤镜预设数值由数值优化得出（模拟浏览器 hue-rotate 矩阵，在母版服装像素
上网格搜索拟合目标色），调色时改 `assets/…/daily-dressup-asset.html` 的
COLORS 表即可。

**步骤 5 · 深色 ×4（输入 = 步骤 2 产物上色 outfit；生图后白→alpha 键出）**：

```
Edit this fashion illustration: recolor only the garment — the loose
dropped-shoulder top and the high-waisted wide trousers — to {深色句}, keeping
the subtle fabric shading. Keep the pose, outlines, long dark-brown hair,
stiletto heels and pure white background exactly unchanged. Change nothing
else.
```

- 墨绿：`a deep forest green (#3F5340)`
- 藏蓝：`a deep navy blue (#2F3E56)`
- 酒红：`a deep burgundy (#7B2D35)`
- 炭灰：`a charcoal grey (#3A3D42)`

**存为**：`images/color/outfit-{green,navy,burgundy,charcoal}.png`

人物单色风格 = 滤镜几乎只动服装；数值按色板目标微调即可。

## 五、验收标准（每张都要过）

1. **姿态一致性**：头/脚质心 vs 母版 < 画布高 1.5%（发型图头部放宽）
2. **比例**：头身比 8.5–9，肩宽 ≤ 1.5 头宽，重心腿/放松腿可辨
3. **白底纯净**：灰度中位数 >250，四角 >245
4. **上色步加验**：只有服装变色（发/线/鞋/背景原样）；砖红均匀、阴影在
   服装区内、不出界
5. **缩略图可辨**：104×124 下能区分 4 个 look
6. **眼筛三件套**：鞋在、叉腰左侧、（造型步）长发未动

## 六、扩展规则

- **加一个色板色** = 加一条滤镜预设（0 张图）
- **加一个 look** = 1 张编辑（从上色 outfit 分支）
- **加一个发型** = 1 张编辑（从上色 outfit 分支）
- 换季度重制 = 重跑步骤 2–4（约 8 张），步骤 0/1 的线稿锚可复用

## 七、质量基线（2026-09-23 彩色批次验收）

8/8 通过：砖红染色均匀带阴影、四 look 区分度达标（露腰/长裙/西装一眼可辨）、
四发型可辨、锚点除 fit 脚部 2.4%（裙/裤腿部露出差异，放行）与 bun 头部
3.4%（盘发改形，放行）外全部 <1.5%。白底、单人、叉腰侧、鞋全数合格。
