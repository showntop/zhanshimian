# 每日内容 · 素材需求清单

> 用途：内容池里 `poster` / `photo` / `video` 三类需要真实素材，其余
> （`swatch` 色卡 / `diagram` 位置图 / `compare` 对比）由程序化绘制，**不需要素材**。
> 这份清单只覆盖"程序化替代不了"的部分。

## 一、交付格式

| 类型 | 比例 | 最小尺寸 | 格式 | 其它 |
|---|---|---|---|---|
| 面料 / 材质特写 | 4:3 横 | 1600×1200 | WebP（首选）/ JPG | 无文字、无水印 |
| 单品图 | 3:4 竖 | 1200×1600 | WebP / PNG（透明底可选） | 平铺或隐形人台，居中 |
| 教程视频 | 9:16 竖 | 1080×1920 | H.264 MP4 | ≤15s、静音、**无字幕**（字幕由 App 叠加） |

命名：`fabric-<材质>-<light>.webp` / `item-<品类>.webp` / `howto-<动作>.mp4`

## 二、通用约束（每条提示词都要带上）

**必须满足，否则不能用：**

- **不露脸、不出现可识别的人**（项目不做人脸相关处理）
- **无品牌 logo、无吊牌、无价格标签**
- **无文字**（说明文字由 App 渲染，图上不要有）
- **不做身材展示/评价**——拍衣服和面料，不拍人体
- 背景干净、中性（浅灰 / 米白 / 深灰），方便在海报里叠加

**通用负面提示词（每条都追加）：**
```
text, watermark, logo, brand label, price tag, human face, person, hands with nails,
full body, model, distorted fabric, over-saturated, harsh flash, cluttered background
```

## 三、P0 · 必需（没有就跑不通）

### A-01 哑光黑面料
```
Close-up macro of matte black wool fabric, soft diffuse studio light, clearly visible
woven texture, subtle depth of field, no sheen, neutral dark grey background,
product textile photography, high detail --ar 4:3 --style raw
```

### A-02 光泽黑面料
```
Close-up macro of black fabric with layered sheen, soft directional light creating
gentle highlights across the surface, visible weave, dark neutral background,
product textile photography, high detail --ar 4:3 --style raw
```
> A-01 / A-02 必须成对：同一材质、同一光位、同一构图，**唯一差别是有无光泽**。
> 它要说明"同样的黑，差在材质"，构图和光不一致就讲不清。

### A-03 卷袖教程（视频，12 秒 · 两步）
```
分镜 1（0–5s）：袖口向上翻一次，停在肘下
分镜 2（5–12s）：再把袖口折回，刚好盖住第一折的边缘
```
```
Overhead close-up of rolling a shirtsleeve, two slow deliberate steps, soft natural
light, plain light background, only forearm and cuff visible, no face, no torso,
calm editorial tutorial style, 9:16
```
> 不露脸、只拍小臂和袖口。字幕由 App 加，视频里不要有。

## 四、P1 · 重要（撑起单品类和面料类）

### B 系列 · 面料对比（成对产出，同样要求同光位同构图）

| 编号 | 内容 | 提示词要点 |
|---|---|---|
| B-01 / B-02 | 粗针 vs 细针 | `chunky knit vs fine-gauge knit, same yarn color, same framing` |
| B-03 / B-04 | 厚毛呢 vs 薄毛呢 | `thick wool melton vs lightweight wool, one structured one draping` |
| B-05 / B-06 | 哑光 vs 光泽（浅色版） | 同 A-01/02，但换成米白/浅灰，用于浅底海报 |

示例（B-01）：
```
Macro close-up of chunky knit wool, thick yarn, clearly visible loops, soft natural
light, neutral beige background, textile photography, ultra detailed --ar 4:3
```

### C 系列 · 单品图（隐形人台或平铺，3:4 竖）

| 编号 | 品类 | 提示词要点 |
|---|---|---|
| C-01 | 结构肩西装 | 肩缝清晰、肩点明确 |
| C-02 | 落肩外套 | 肩缝明显掉到上臂 |
| C-03 | 高领毛衣 | 领口高度可见 |
| C-04 | V 领针织 | 用于领口对比 |
| C-05 | 直筒长裤 | 裤脚落点可见 |
| C-06 | 中长款大衣 | 下摆落点可见 |

示例（C-01）：
```
Minimal product shot of a structured-shoulder wool blazer on invisible mannequin,
clean light grey background, soft even studio light, full garment centered, sharp
shoulder seam visible, editorial e-commerce photography, no brand, no label --ar 3:4
```

> C-01 与 C-02 也是一对（正肩 vs 落肩），构图与光位必须一致。

## 五、P2 · 增强（有则更好，没有也能跑）

- 更多面料：亚麻、真丝、牛仔、羊绒、皮革
- 更多单品：包（用于"包背在哪"）、鞋、围巾
- 教程视频：塞衣角、系腰带、叠穿
- 纸张/布纹底纹（可选，目前是程序化生成，够用）

## 六、一个提醒

**不要为了"素材看起来高级"而拍带场景的大片。**

这些图在海报里是被裁切、旋转、叠字使用的，**干净的中性棚拍比特写大片好用得多**。
判断标准：把图缩到 400px 宽、压在深色块上、上面盖两行字——还能看清它在讲什么，才算合格。
