# 「怎么打扮」整体实施方案：小程序 · 服务端 · 手机端（二期）

- 版本：v1.0（2026-09-08）
- 事实基线：`main@002d35c`（另有工作区未提交的首页任务卡改动与 `design-qa.md` 更新）
- 读者：本项目开发者与协作者；文档目标是**可以直接照此开工**：先盘点现状，再补齐原型与实现的差距，最后规划手机端二期。
- 与现有文档的关系：
  - `docs/technical-design.md`（第一阶段技术方案）继续有效，本文档不重复其安全/部署细节，只做增量与汇总；
  - `docs/ai-provider-architecture.md`（AI 能力路由）、`docs/ai-evaluation.md`（评测与灰度）是 AI 部分的细则来源；
  - `AGENTS.md` 是原型的运行时契约与产品决策记录，`brand/README.md` 是品牌规范；
  - **本文档是三端（小程序 / 服务端 / 手机端）的实施总纲**，冲突时以本文档为准，并回写更新。

---

## 1. 产品定义

### 1.1 定位与品牌

| 项 | 内容 |
|---|---|
| 品牌名 | **怎么打扮**（2026-09-04 确认，取代早期「见我」；原型文案中出现的「见我」一律视为旧稿） |
| 定位 | 懂发型、妆容和穿搭的 **AI 形象顾问**；是私人顾问，**不是颜值打分工具** |
| 服务范围 | 发型、妆容、穿搭与场合造型 |
| 识别策略 | 完整名称用于产品与推广；单字「扮」用于小程序图标 / App 图标 / 社交头像 |
| 一句话简介 | 上传照片，获得适合你的发型、妆容、穿搭与场合造型方案 |
| 社交简介 | 不追潮流模板，只帮你找到更适合自己的打扮方式 |

### 1.2 用户与核心闭环

核心人群（增长文档已定义）：求职应届生（21–25）、职场新人（25–30）、晋升/转行期（28–38）女性为主。

产品闭环分两层：

- **首次主闭环**：`首页 → 三图建档 → 补充资料 → 异步分析 → 当前形象报告 → 三套方案 → 方案详情/前后对比 → 改造清单 → 实拍反馈 → 档案复用`
- **复访闭环**（四条，均复用已有档案，不重复收集照片）：今日造型、场合方案（面试/婚礼/约会/日常）、衣橱 Lite、私人顾问；另有三个快捷工具（发型预览、穿搭诊断、购买判断）与方案分享卡。

### 1.3 设计红线（不可违反）

1. **不打颜值分/身材分**，不做身材羞辱，不做医学结论，不用警示红问题标签；一律用「可提升点」这类尊重且可执行的表达。
2. **AI 生成图像必须显式标识**（「AI 风格预览」「效果示例」「风格参考」），依据《人工智能生成合成内容标识办法》（2025-09-01 施行）。
3. **数据真实性**：绝不静默回退假内容——API 失败/为空必须渲染明确的空态/错误态；Demo 内容只经服务端 `/v1/media/demo` 与 Demo Provider 进入，绝不客户端注入。
4. **照片与建议只对用户本人可见**：私有对象存储 + 短时签名 URL；提供完整的「删除我的数据」。
5. **首页必须保留**「你好，我是你的私人形象顾问」语境；场景叫「日常」不叫「通勤」。
6. 产品结构是**原生 APP 工作台**，不是品牌落地页：紧凑任务型标题、可见状态、复用入口、常驻底导航（首页/方案/我的，不加商城/社区/发现）、高密度卡片；避免超大 slogan、长营销文案、装饰性 hero。

---

## 2. 现状基线与差距

### 2.1 已交付能力（已核实）

**小程序**（`miniapp/`，原生微信小程序，AppID `wx911e0fbcba0b24d0`）：

- 18 个页面全部主包（`lazyCodeLoading: requiredComponents`），原生 3 项 tabBar（首页/方案/我的），全局自定义导航 `app-header`。
- 主闭环完整：capture（三照采集，含连续拍摄/相册/单槽重拍/示例体验）→ analysis（700ms 轮询 + 扫描线动效 + 照片检查失败逐图原因）→ report（标注归位到来源照片、脏字符清洗）→ plans（三方案轮播 + 本人图异步生成 + 2500ms 轮询）→ plan → checklist（乐观更新+失败回滚）→ feedback（实拍必上传，不静默丢弃）。
- 复访闭环完整：today（天气上下文 + 本人生成图轮询）、share（canvas 海报 + 公开 token + 撤销）、wardrobe（8 件上限 + 搭配生成）、advisor（会话恢复 + 动作应用）。
- 数据真实性契约落在 `utils/media.js`：`lookImage()` 严格返回空、`exampleImage()` 唯一内置图入口必须带 `.example-badge` + `.example-soft`、`userImage()` 用户图失败保持空。
- 401 自动重登重试一次；开发环境 `http://` 图片下载到 `USER_DATA_PATH` 本地化（微信 3.17+ 拒绝 http 图）。

**服务端**（`server/`，Go 1.24 模块化单体，标准库 `net/http` + pgx/v5 + PostgreSQL 16）：

- 41 个 HTTP 端点（`{data}` / `{error:{code,message,request_id}}` 封装，Bearer 会话鉴权，除公开端点外全覆盖）。
- 会话：`wx.login → jscode2session`，服务端只存 token 的 SHA-256 摘要，TTL 30 天；`DELETE /v1/me/data` 单事务删 13 张表 + 异步清理对象存储。
- 异步任务 4 类（analysis / hair_preview / plan_look / today_plan_look）：`FOR UPDATE SKIP LOCKED` + 10 分钟僵尸回收 + 分级重试上限（分析 3 次、生成类 2 次）+ 永久性错误立即终止；API 进程可内嵌 worker（`RUN_WORKER=true`），生产同镜像 `worker` 命令独立扩容。
- AI 能力路由：9 个能力常量、6 种协议适配器（openai_responses / openai_chat_completions / openai_image_edit / dashscope_wan / dashscope_wanx_imageedit / ark_image）、按路由 `max_cost_cny` 成本守门、`InvocationMeta` 全链路结构化日志、严格 JSON Schema + 服务端二次校验（含 `safeText` 措辞护栏与锚点间距分离）。
- 生产启动门禁（`APP_ENV=production`）：禁开发登录、强制 COS 私有存储 + 短时签名 URL、强制高德天气、强制 AI 路由配置且 6 能力齐全、密钥只从环境注入。
- 端到端回归：`server/scripts/e2e.sh` 覆盖主闭环、四复访闭环、跨用户越权（期望 404）、注销删除完整性。

### 2.2 原型功能差距清单（本方案要补齐的）

对照 `src/Prototype.tsx`（视觉与流程唯一事实来源）逐屏核对，以下是**原型有、线上实现缺**的项：

| # | 差距 | 原型位置 | 影响 | 优先级 | 计划归属 |
|---|---|---|---|---|---|
| G1 | **补充资料步骤缺失**：身高步进器 + 职业 + 预算（03/05 步）；小程序 `createAnalysis` 直接传 `profile:{}` 跳过 | `makeProfileScreen` | AI 缺少 grounding，报告与方案个性化打折 | P0 | M0（服务端 `CreateAnalysisInput.profile` 已支持，前端补步骤即可；同时新增持久化接口见 §5.3） |
| G2 | **发型师参考卡缺失**：plan 页「分享给发型师」BottomSheet（长度/刘海/卷度参数 + 保存参考卡） | `makePlanScreen` | 清单闭环的关键交付物，用户去理发店的实际抓手 | P0 | M0（数据放在 `plan_steps.details` jsonb，前端补 UI） |
| G3 | **反馈成功态的个性化承诺文案**（引用所选方案名：「下一次推荐会优先保留『精神利落』的发型轮廓与肩线」） | `makeFeedbackScreen` 成功态 | 闭环感与复访动机 | P1 | M0 |
| G4 | **品牌色与 token 漂移**：小程序 `--moss:#567243` / `--ink:#292d29` / 页面底 `#f8f7f3`，品牌与原型为 `#587344` / `#252725` / `#F8F5F0` | `prototype.css` vs `brand/README.md` | 两端（小程序/手机端）视觉不一致的根因 | P1 | M0 统一 token（§4.2），改动后跑 design-qa |
| G5 | **体验实验室只有静态占位**（AR/3D/试衣均为 modal 提示 + 等待名单） | `makeLabScreen` | 原型定位「放哇塞」；当前无真实能力 | P2 | M3（以身份一致性评测通过为前置，见 §8.3） |
| G6 | **身体数据选填（体重/三围）无落点**：原型「我的档案」展示「体重与三围选填」，服务端 `Profile` 只有 `height_cm/role/budget` | `makeArchiveScreen` | 选填数据的持久化与再利用 | P2 | M2（随 §5.3 `me/profile` 接口） |
| G7 | 原型 `analysis` 页的分段文案节奏（<55% 读比例 → <90% 匹配场景 → ≥90% 已备好）与小程序文案未完全对齐 | `makeAnalysisScreen` | 体验细节 | P2 | M2 |

小程序已超出原型的部分（任务卡横轨、今日造型卡、顾问入口、桌面通知式轮询恢复等）**保留**，不回退。

---

## 3. 总体架构

### 3.1 系统拓扑（二期）

```
┌──────────────┐   ┌──────────────────┐
│ 微信小程序     │   │ 手机端 App（二期） │      iOS / Android
│ 原生 WXML/WXSS│   │ Expo + RN + TS   │
└──────┬───────┘   └────────┬─────────┘
       │  wx.login code     │  手机号/Apple/微信App OAuth
       ▼                    ▼
┌─────────────────────────────────────────────┐
│                Go API（模块化单体）             │
│  httpapi → service → repository / provider / │
│                     storage                  │
│  · 统一 /v1 契约、Bearer 会话（多端身份）        │
│  · AI 能力路由（9 能力 × 6 协议 × 成本守门）      │
│  · Worker ×4（SKIP LOCKED 队列，可独立部署）     │
└───────┬──────────────┬──────────────┬────────┘
        ▼              ▼              ▼
  PostgreSQL 16   私有 COS/OSS    外部 Provider
  （业务+队列）    （照片，签名URL）  （AI/天气/微信/短信）
```

要点：

- **一套服务端、多端客户端**。二期不拆微服务（与 technical-design §11 一致）：单日任务量未到单库队列瓶颈前，保持模块化单体；拆分触发条件写在该节。
- **客户端矩阵**：

| | 小程序（一期，已上线迭代） | 手机端（二期） |
|---|---|---|
| 技术栈 | 原生 WXML/WXSS/JS，无编译链 | Expo（React Native）+ TypeScript |
| 登录 | 微信 code | 手机号验证码 / Apple / 微信 App OAuth（§9.5） |
| 图片 URL | 允许 `/assets/...` 相对路径（包内资源） | 一律绝对 URL（客户端拼 `API_BASE_URL`） |
| 推送 | 模板消息/订阅消息（按需） | APNs 优先，Android 厂商通道后置（§9.6） |
| 迭代方式 | 微信审核发布 | App Store / 国内安卓市场 + EAS Update 热更 |

- **架构分层原则**（服务端）：`httpapi` 只做协议翻译与校验；`service` 持有全部业务规则与事务边界；`repository` 每次读写同时校验资源 ID + user_id（防越权）；`provider` 隔离一切外部世界（AI/天气/微信/短信），业务只依赖接口；`storage` 隔离对象存储。新增能力时**只允许**在这些缝隙里扩展。

### 3.2 AI 能力路由（沿用并扩展）

业务只依赖 9 个能力常量：`appearance_analysis / photo_check / outfit_diagnosis / purchase_diagnosis / advisor_chat / today_plan / hair_edit / makeup_edit / full_look_edit`。换厂商只改 `server/config/ai-routing.*.json`，不改业务代码。

二期路由原则（照搬 `docs/ai-provider-architecture.md` 并新增一条）：

1. 形象分析以效果为先：固定评测集胜出才能换主路由；
2. 顾问/诊断以性价比为先，但校验不降；
3. 图片编辑必须锁定身份（发型已上线；**妆容/换装在身份一致性评测通过前不开放产品入口**）；
4. 每条路由至少一个跨厂商回退 + 请求前 `max_cost_cny` 拦截；
5. **二期新增**：手机端用户量放大后，`advisor_chat` 增加按用户维度的并发限流（复用 `pg_advisory_xact_lock` 思路或应用内信号量），防止单用户刷爆预算。

---

## 4. 设计系统（UI/UX）

### 4.1 设计原则

- **暖珍珠白 + 石墨 + 苔藓绿**的编辑感：克制、留白、发丝描边；卡片主要靠 1px 半透明线分层，不做重投影。
- **中文标题用系统宋体（Songti SC / STSong），正文用系统无衬线（PingFang SC 等），最多两套字体**。
- **8pt 间距体系**；主按钮最小高 96rpx（48px），触控热区 ≥88rpx（44px）。
- **动效只用于反馈与空间关系**：页面淡入上移、扫描线、进度呼吸、卡片选择、清单勾选弹性；全局响应 `prefers-reduced-motion`。
- 图片永远标注「当前 / 方案 / AI 预览 / 风格参考」，身份归属不允许歧义。

### 4.2 Design Tokens（单一事实来源）

二期起，token 以本表为准（**品牌值为准，M0 中把小程序实现值对齐过来，见 G4**）；手机端直接以本表生成 `theme.ts`，小程序以本表维护 `app.wxss` 的 `page` 选择器变量：

| Token | 值 | 现小程序值 | 语义 |
|---|---|---|---|
| `--bg` | `#F8F5F0` | `#f8f7f3`（页面）/ `#f8f5f0`（窗口） | 暖珍珠白页面底 |
| `--surface` | `rgba(255,255,255,.78)` | 同 | 半透明卡片面（玻璃拟态基座） |
| `--surface-strong` | `#FFFFFF` | 同 | 实面 |
| `--ink` | `#252725` | `#292d29` | 主文字（石墨） |
| `--ink-2` | `#656B64` | 同 | 次级文字 |
| `--ink-3` | `#747A73` | 同 | 三级/eyebrow 文字 |
| `--moss` | `#587344` | `#567243` | 品牌主色（苔绿） |
| `--moss-pressed` | `#486238` | 同 | 主色按压 |
| `--moss-soft` | `#EEF2E9` | `#edf2e9` | 主色浅底（摘要条/选中 chip） |
| `--line` | `rgba(69,78,64,.14)` | 同 | 发丝描边 |
| `--danger` | `#9B4B45` | 同 | 删除确认等危险操作 |
| `--warn` | `#9B6D58` | 同 | 失败/注意（暖赭，非警示红） |
| `--badge` | `rgba(30,35,29,.62)` | 同 | 示例角标底 |

圆角：`--radius-sm 16rpx / md 20rpx / lg 24rpx / xl 28rpx`，胶囊 `999rpx`；分析页人像卡特殊造型 `120px 120px 24px 24px`（拱门形）。

排版：`.serif` 标题宋体；`.title` 50rpx/700/-1rpx/1.26；`.section-title` 32rpx/700；`.eyebrow` 23rpx/600/字距 4rpx/`--moss`；`.lede` 26rpx/1.65；正文基准 28rpx。工作台密度区（首页）允许下探到 strong 30rpx、note 20rpx。

阴影：主按钮 `0 12rpx 28rpx rgba(75,100,57,.16)`；工作台卡 `0 6px 18px rgba(52,59,47,.05)`；弹层 `0 -16px 48px rgba(0,0,0,.2)`；其余一律发丝描边不投影。

### 4.3 动效系统（具体参数，两端共用）

**全局微交互**

| 名称 | 参数 | 用途 |
|---|---|---|
| `fade-up` | `.52s cubic-bezier(.2,.8,.2,1)`，opacity 0→1 + translateY 22rpx→0；delay-1/2/3 = .08/.16/.24s | 页面区块入场 |
| `pressable` | `transform .16s ease, opacity .16s ease`；active `scale(.985) opacity(.88)` | 所有可点元素 |
| `shimmer` | `1.3s infinite`，高光 translateX(-100%→100%) | 骨架屏 `.skeleton` |
| `scan` | `2s ease-in-out alternate`，translateY 扫描 | 分析页扫描线（辉光 `0 0 18px 5px rgba(111,143,89,.24)`） |
| `pulse` | `1.6s`，scale 1.28 + opacity .72 | 分析页焦点点 |
| `check-pop` | `.25s`，scale 1→1.18→1 | 清单勾选 |
| `previewFade` | `.3s`，from opacity .45 + scale 1.012 | 方案大图切换 |

**页面转场**（原型 FlowStack 实测参数；小程序沿用系统转场，手机端用 Reanimated 复刻）：

- push/pop spring：`stiffness 360 / damping 38 / mass 0.9`；push 新屏自右 `x:100%` 进入，旧屏退至 `-28%` 并 `scale .985`（iOS 叠层后退）。
- 边缘右滑返回：起始触点左缘 28px 内，跟手位移；松手阈值 `x > 92px` 或 `velocity > 0.45`。
- BottomSheet：进 spring `500/43/0.9`，出 spring `250/30/1.05`（更慢更重）；下拉关闭阈值 `y > 96px` 或 `velocityY > 0.55`。
- 键盘/底部安全区联动：`.26s cubic-bezier(.2,.8,.2,1)`。
- 滚动物理（手机端）：`momentumFriction 2.1 / velocityScale 890 / bounceTension 200 / bounceFriction 40 / overdragScale 0.5（上限 96px）/ tapSlop 8px`；拖拽后 180ms 内抑制 click。

**规约**：`prefers-reduced-motion: reduce` 时全部动画压到 0.01ms（两端都要实现）；分析页进度补间保持「显示进度追真实进度」的双轨（服务端推进度 + 前端 500ms 补间），不许跳变。

### 4.4 组件库清单（两端映射）

| 组件 | 小程序 | 手机端（RN） | 说明 |
|---|---|---|---|
| 自定义导航 | `components/app-header`（真机测量状态栏+胶囊） | `app-header.tsx`（SafeArea + 桥接系统返回手势） | 左返回/字标「怎么打扮」、右 help/share/profile 槽 |
| 主按钮 | `components/primary-button`（loading/disabled 吞点击） | `primary-button.tsx` | min-height 96rpx，苔绿投影 |
| 示例角标+弱化 | `.example-badge` + `.example-soft`（blur 1.2–4px） | 同名组件 | 数据真实性契约的 UI 落点，AI/示例图必挂 |
| 骨架屏 | `.skeleton`（shimmer） | `skeleton.tsx` | 所有加载态首选，不用转圈 |
| chip/pill | `.pill` | `chip.tsx` | 单选组、反馈词、场合选项 |
| 对比切换 | plans `toggleCompare` / plan `showCurrent` | `compare-toggle.tsx` | 「原本/方案」玻璃拟态分段控件 |
| 空态/错误态 | 各页内联 | `empty-state.tsx / error-state.tsx` | 必须给出下一步动作（重试/返回），文案不甩锅 |
| 底部弹层 | 页面内 bottom-sheet（衣橱表单） | `@gorhom/bottom-sheet` | 与原型 spring 参数一致 |
| 任务卡横轨 | home 横向 scroll-view | `task-rail.tsx` | 进行中任务聚合（analysis/plan-look/hair） |
| 光箱预览 | home/today lightbox | `image-viewer.tsx` | 大图查看 + 关闭 |

### 4.5 图像与 AI 标识规范

1. 包内资源只发 JPEG（母版 PNG 由 `project.config.json` 的 `packOptions.ignore` 排除；图标 SVG 母版 → `rsvg-convert` 生成 PNG 引用）；**禁 WebP**（微信渲染空白）。
2. 服务端返回的相对路径 `/assets/...` 是包内/镜像内资源；`/uploads/...` 是用户与生成内容（COS 短时签名）；手机端把所有相对路径拼 `API_BASE_URL`。
3. AI 生成图一律挂角标：本人预览「AI 风格预览」、Demo 结果「效果示例，仅供参考」、包内模特「风格参考」；`provider_version` 以 `demo` 开头时前端强制按示例处理（现约定保留）。
4. 分享卡 snapshot 只存相对路径，读取时动态签名（已实现，保持）。

### 4.6 无障碍与降级

- 触控热区 ≥44px；文字对比度按石墨/暖白基线已达标，新配色需过 WCAG AA（正文 ≥4.5:1）。
- `prefers-reduced-motion` 全局生效；动效只加强不承载信息（进度永远有数字与文案）。
- 弱网：所有轮询页有失败计数上限与明确失败态；上传失败可重试且不重复扣任务；离线时首页缓存上次数据并标注「当前展示的是上次内容」。

---

## 5. 接口契约（前后端交互规范）

### 5.1 通用约定

- 前缀 `/v1`，JSON；成功 `{"data": ...}`，失败 `{"error":{"code","message","request_id"}}`；每响应带 `X-Request-ID`。
- 鉴权 `Authorization: Bearer <token>`（除 `GET /healthz`、`POST /v1/auth/*`、`GET /v1/share/{token}`）；401 统一语义「登录已失效」，客户端清 token 重登后重放一次。
- 错误码：`validation_error`(400) / `unauthorized`(401) / `forbidden`(403) / `not_found`(404，越权一律 404 不泄露存在性) / `wechat_code_invalid`(401) / `wechat_login_limited`(429) / `wechat_unavailable`(502) / `internal_error`(500)。
- 语义化 URL：资源用名词复数，动作用动词子资源（`/select`、`/activate`、`/apply`、`/revoke`、`/save`、`/wear`）；状态查询用 GET，创建异步任务返回 202 + 可轮询资源。
- 异步契约：创建 → `202 + {status:"queued"}` → 客户端轮询 GET → `completed`（带结果 URL）/ `failed`（带中文 `error_message`，照片不合格时逐图给原因）。**客户端轮询间隔规范：分析 700ms、生成图 2500ms、发型预览 900ms、今日 3000ms；连续失败 5 次进失败态。**
- 图片 URL 规则：见 §4.5 第 2 条；签名 URL 过期由服务端 `resolveAssetURL` 自愈，客户端不处理签名。

### 5.2 现有端点总表（已实现，41 个）

| 分组 | 端点 |
|---|---|
| 基础 | `GET /healthz`；`POST /v1/auth/dev`（仅开发）；`POST /v1/auth/wechat` |
| 媒体 | `POST /v1/media`（multipart，kind: face/side/body/feedback/outfit/product/wardrobe）；`POST /v1/media/demo` |
| 分析与报告 | `POST /v1/analyses`(202)；`GET /v1/analyses/{id}`；`GET /v1/reports/current`；`GET /v1/reports/{id}` |
| 方案 | `GET /v1/reports/{id}/plans?scene=`；`POST /v1/reports/{id}/plan-looks?scene=&refresh=`(202)；`POST /v1/reports/{id}/scene-plans`；`GET /v1/plans/{id}`；`POST /v1/plans/{id}/select` |
| 清单与反馈 | `GET /v1/plans/{id}/checklist`；`PATCH /v1/checklist/{id}`；`POST /v1/feedback` |
| 工具 | `POST /v1/tools/run`（hair/outfit/purchase）；`POST /v1/tools/{id}/save` |
| 发型预览 | `POST /v1/hair-previews`(202)；`GET /v1/hair-previews`；`GET /v1/hair-previews/{id}`；`POST /v1/hair-previews/{id}/save` |
| 今日 | `GET /v1/today/context`；`GET /v1/today/plans/current`；`POST /v1/today/plans`；`POST /v1/today/plans/{id}/activate`；`POST /v1/today/plans/{id}/feedback` |
| 分享 | `POST /v1/share-cards`；`GET /v1/share/{token}`（公开）；`POST /v1/share-cards/{id}/revoke` |
| 衣橱 | `GET/POST /v1/wardrobe/items`；`DELETE /v1/wardrobe/items/{id}`；`POST /v1/wardrobe/outfits`；`POST /v1/wardrobe/outfits/{id}/wear` |
| 顾问 | `GET /v1/advisor/conversations/{id}/messages`；`POST /v1/advisor/messages`；`POST /v1/advisor/actions/{id}/apply` |
| 埋点与隐私 | `POST /v1/events`；`DELETE /v1/me/data`(204) |

字段级契约见 `server/internal/domain/domain.go`（唯一权威定义）；联调以 `/healthz` 的 `ai_routes` 与 provider 版本核对环境。

### 5.3 二期新增 / 变更接口

| 端点 | 用途 | 说明 |
|---|---|---|
| `GET /v1/me` | 当前账户 | `{id, nickname, created_at, identities:["wechat_miniapp","phone",...]}`，手机端「我的」页与多端身份展示 |
| `GET /v1/me/profile` / `PUT /v1/me/profile` | **持久化补充资料**（G1/G6） | `{height_cm?, role?, budget?, weight_kg?, body_metrics?}` 全选填；建档流程写入 + `POST /v1/analyses` 快照进 `analyses.profile`；衣橱搭配、今日方案把它加进 grounding |
| `POST /v1/auth/sms/request` | 发送验证码 | `{phone}`；60s 冷却、按 IP+号码限流（登录 10/min/IP、验证码 5/hour/号码）、验证码 5 分钟有效、存摘要不存明文 |
| `POST /v1/auth/sms/verify` | 验证码登录 | `{phone, code, nickname?}` → `Session`；首登自动建用户并合并同手机号身份 |
| `POST /v1/auth/wechat-app` | 微信 App OAuth（Android 端） | `{code}`，走开放平台 access_token+openid；接口与 `wechat` 登录复用建户逻辑 |
| `POST /v1/auth/apple` | Apple 登录（iOS） | `{identity_token, nickname?}`；服务端验 JWKS（`appleid.apple.com/auth/keys`）取 `sub` |
| `DELETE /v1/auth/sessions/current` | 退出登录 | 删除当前 session 行（手机端「退出登录」） |
| `PUT /v1/me/push-token`（P1） | 注册推送 | `{platform, token}`；配合 §9.6 |

**鉴权语义变更（保持向后兼容）**：`users` 不再只绑定 `open_id`，引入 `user_identities(provider, identifier UNIQUE, user_id)`（§6.2）。`POST /v1/auth/wechat` 行为不变（provider=`wechat_miniapp`）。

**契约治理**：

1. 新增 `contracts/openapi.yaml`（OpenAPI 3.1），以 §5.2/5.3 为初始内容；CI 用 spectral lint + 一个「契约示例回归」测试（从 yaml 生成响应示例与 `domain.go` 序列化结果比对）。
2. 手机端用 `openapi-typescript` 生成 TS 类型；小程序保持手写 JSDoc（无编译链约束），但 `services/api.js` 的方法注释必须与 yaml 同步，validate.mjs 增加「api.js 路径 ↔ openapi.yaml paths」一致性检查。
3. 破坏性变更必须升 `/v2`；`/v1` 内只做加字段。

---

## 6. 数据模型

### 6.1 现有表（PostgreSQL，11 个迁移已上线）

`users / user_sessions(token_digest) / media_assets(软删) / analyses + analysis_jobs / reports + report_findings / plans(+scene, scene_brief, generation_*列) + plan_steps / checklist_items / feedback(+media_id) / tool_results / hair_previews(任务状态合一) / today_plans(UNIQUE(user_id, plan_date), generation_*列) / share_cards(token, snapshot jsonb, 撤销+过期) / wardrobe_items + wardrobe_outfits / advisor_conversations + advisor_messages + advisor_actions / product_events / schema_migrations`

关键约束已内建：所有业务表带 `user_id`；`(report_id, scene, slug)` 唯一；`(user_id, plan_date)` 唯一；删除走 `DeleteUserData` 依赖序事务。

### 6.2 二期 schema 演进（新增迁移，全部向后兼容）

```sql
-- 012_multi_channel_identity.sql
CREATE TABLE user_identities (
  id uuid PK, user_id uuid FK->users CASCADE,
  provider text CHECK (provider IN ('wechat_miniapp','wechat_app','apple','phone')),
  identifier text NOT NULL,            -- openid / apple sub / E.164 手机号
  created_at timestamptz DEFAULT now(),
  UNIQUE (provider, identifier)
);
-- users.open_id 列保留（兼容存量微信用户），启动时做一次性回填到 user_identities

-- 013_user_profiles.sql
CREATE TABLE user_profiles (
  user_id uuid PK FK->users CASCADE,
  height_cm int, role text, budget text,
  weight_kg numeric, body_metrics jsonb,   -- 选填（G6）
  updated_at timestamptz
);

-- 014_sms_and_devices.sql
CREATE TABLE sms_codes ( phone text, code_digest bytea, purpose text,
  expires_at timestamptz, used_at timestamptz, created_at timestamptz );
CREATE TABLE device_tokens ( id uuid PK, user_id uuid FK->users CASCADE,
  platform text CHECK (platform IN ('ios','android')), token text UNIQUE,
  updated_at timestamptz );
```

删除语义扩展：`DeleteUserData` 增删 `user_identities / user_profiles / sms_codes(按 user 关联) / device_tokens`；对象存储清理列表不变。

---

## 7. 小程序实施（一期收口 + 差距修复）

### 7.1 工程约定（保持）

- 无编译链：JS + JSDoc；页面只调 `services/api.js`；状态 `App.globalData` + `wx.setStorageSync`；`navigationStyle: custom` 全局。
- 硬规则（历史事故沉淀，**不可违反**）：WXML 条件链用 `wx:elif`（`wx:else-if` 会被工具链静默忽略）；图片禁 WebP；`utils/media.js` 三函数契约；错误态与内容不同屏；tab 页用 `skipNextShow`/`reportRecovered` 防重复加载与闪屏。
- 每次 UI 改动跑 `node miniapp/scripts/validate.mjs`（页面/组件四件套、WXML 非法方法、本地资源存在性）+ `node --check`；视觉改动走 `design-qa.md` 验收记录流程。

### 7.2 轮询与任务恢复规范（保持并统一）

| 页面 | 间隔 | 恢复机制 |
|---|---|---|
| analysis | 700ms + 500ms 显示补间 | `jianwo_active_analysis_id`，onLoad 恢复 |
| plans | 2500ms | `jianwo_active_plan_generation`，allReady 清除 |
| hair | 900ms | `jianwo_active_hair_preview`，resumePreview |
| today | 3000ms | current 接口自然恢复 |

统一要求：`onUnload` 必清 timer；任务互斥用「单一活跃任务 key」；失败态给出可执行动作（重试/重新拍摄/返回）。

### 7.3 差距修复（对应 §2.2）

- **G1 补充资料**：`capture` 完成后插入 `pages/profile-setup/`（复用原型 stepper + chips 规格：身高 145–185、职业/预算 chips，全部可跳过）；`createAnalysis` 带上 `profile`；同时 `PUT /v1/me/profile` 持久化。文案照原型（「少一点填写，多一点准确」「体重与三围不是必填项」）。
- **G2 发型师参考卡**：`pages/plan/` 增加「分享给发型师」→ bottom-sheet（方案图 + 长度/刘海/卷度，来源 `plan_steps.details.hair_spec`，服务端 Demo/OpenAI 输出 schema 增补该字段，缺失时隐藏入口）；「保存参考卡」→ 相册。
- **G3 反馈成功态**：`done` 视图替换为原型成功态（圆形对勾 + 「第一次闭环完成」+ 引用方案名的个性化文案）；文案模板由服务端 `POST /v1/feedback` 响应返回（`message` 字段），保持"我们记住了什么对你有效"的语义由服务端统一产出。
- **G4 token 统一**：`app.wxss` 与各页面硬编码色值替换为 §4.2 规范值；改动后跑一轮 design-qa 对照 `qa/app-home-v2.png` 基线。
- **G7 分析文案对齐**：`analysis` 阶段文案改为服务端 `stage` 驱动 + 前端分段映射，去掉客户端自造百分比口径。

---

## 8. 服务端实施（一期加固 + 二期扩展）

### 8.1 分层与代码规约（保持）

`cmd/api`（可内嵌 worker）/ `cmd/worker`（生产独立部署）；`httpapi → service → repository/postgres | provider/* | storage/*`；依赖只有 uuid、pgx/v5、COS SDK。新增功能必须沿缝隙插入，禁止 handler 直查数据库。

### 8.2 异步任务体系（保持，两点加固）

现表：4 类任务、SKIP LOCKED、10min 僵尸回收、分级重试、`guardWorkerJob` panic 隔离、`failContext`(10s)/`writeContext`(1min) 分离回写。加固：

1. **队列可观测**：`/healthz` 增加 `jobs:{oldest_queued_seconds, failed_last_hour}`（一条 SQL 聚合），对齐 technical-design §9 的「队列最老任务 >5 分钟」告警线；
2. **轮询周期可配**：`AnalysisPollTime` 从硬编码 700ms 改为 env（默认不变），多实例扩容时留调节空间。

### 8.3 Provider 与评测门禁（保持）

- 评测按 `docs/ai-evaluation.md` 执行：自动门槛（Schema 通过 ≥98%、数量正确 100%、禁止措辞 0、锚点 100%、P95 ≤30s、非预期回退 0）+ 人工盲评（安全项必须 5 分）+ 灰度 5%→25%→100%。
- **妆容图（makeup_edit）与换装图（full_look_edit）的产品入口**在身份一致性盲评（同分不同人混淆率）达标前保持关闭——这是体验实验室（M3）的前置条件，不是并行项。
- 三套方案静态人物图永远标「效果示例」。

### 8.4 新增实施项（二期）

1. **多端身份**：`user_identities`（§6.2）；`WeChatLogin` 建户逻辑抽为 `ensureUserByIdentity(provider, identifier, nickname)` 复用到 sms/apple/wechat-app；同一手机号/微信跨端登录映射同一 `user_id`（存量小程序用户数据自动可见于手机端，这是二期的核心用户价值）。
2. **短信 Provider**：`internal/provider/sms.go` 定义 `SmsSender.Send(ctx, phone, code)`；实现 `AliyunSms`（阿里云，费用最低）+ `ConsoleSms`（开发打印）；限流在 service 层（IP+号码双维度，§5.3 参数）。
3. **Apple 登录**：`internal/provider/apple.go`，JWKS 缓存 1h，验 `aud`=BundleID、`exp`、nonce 可选；取 `sub` 为 identifier。
4. **会话滑动续期**：`Authenticate` 时若 `expires_at - now < 7d` 则顺手续到 30d（一条 UPDATE，低频触发），手机端长期使用不再 30 天强制重登。
5. **profile grounding**：`today_plan / wardrobe_outfits / advisor` 的 grounding 注入 `user_profiles`（缺失安全降级，同现有 Today/Wardrobe/Report 模式）。
6. **推送（P1）**：`internal/provider/push.go`（APNs token + JWT auth）；场景仅三个：分析完成、方案生成完成、清单提醒（用户自设时间，默认无）；失败静默重试 1 次，推送不可用时业务不受影响。

### 8.5 配置与部署

- 环境变量全集见附录 A；新增：`SMS_*`、`APPLE_*`、`WECHAT_APP_SECRET`（开放平台，与小程序 secret 区分）、`APNS_*`、`ANALYSIS_POLL_MS`。
- 部署形态不变：1 API + 1 Worker + 托管 PG + COS；docker-compose 本地开发；生产门禁自动拦截错误配置。手机端发布不改变服务端拓扑，仅需：API 域名对 App 生效（无微信域名校验白名单问题）、限流基线上调（登录类）。

### 8.6 可观测（保持并对齐）

X-Request-ID 贯通；`InvocationMeta` 按 `provider_version` 分桶；核心漏斗指标（上传成功率/分析完成率 P95/报告→方案点击/方案选择/清单完成/删除成功率）继续走 `product_events`；新增手机端崩溃上报（Sentry 免费档起步）。

---

## 9. 手机端（二期）实施

### 9.1 选型与论证

**选 Expo（React Native）+ TypeScript**。

| 候选 | 结论 |
|---|---|
| **Expo + RN** ✅ | 原型即 React 19 + motion；设计语言（spring 转场/手势/BottomSheet/滚动物理）可 1:1 映射到 Reanimated + GestureHandler；一套代码 iOS+Android；EAS Update 免审核发 JS 层修复；团队现有技能栈直接复用 |
| Flutter | 渲染一致性好，但与现有 React 原型/技能栈断裂，设计还原要重写一遍 |
| 双端原生 | 成本最高，单人团队不可行 |
| H5/PWA | 微信外分发体验与相机/文件权限弱，国内安卓厂商推送缺失，「二期做 App」的价值不成立 |

版本基线：Expo SDK 54+（RN 0.81+，New Architecture 默认）、Expo Router 4、Reanimated 3、GestureHandler 2。

### 9.2 工程架构

```
mobile/                        # 与 miniapp/、server/ 平级
├── app/                       # Expo Router 路由（文件即路由）
│   ├── (tabs)/ home.tsx | plans.tsx | profile.tsx
│   ├── capture.tsx  profile-setup.tsx  analysis.tsx
│   ├── report.tsx   plans/[scene].tsx  plan/[id].tsx
│   ├── checklist.tsx feedback.tsx  today.tsx
│   ├── wardrobe.tsx advisor.tsx  lab.tsx  settings.tsx
├── src/
│   ├── api/                   # openapi-typescript 生成的类型 + 客户端
│   │   ├── client.ts          # fetch 封装：Bearer、401 重登重放、X-Request-ID
│   │   └── hooks/             # TanStack Query：useAnalysis(id) 轮询等
│   ├── theme/ tokens.ts       # §4.2 表的唯一代码化来源
│   ├── ui/                    # §4.4 组件（RN 版）
│   ├── motion/                # 转场/Spring 常量（§4.3 参数）
│   ├── store/                 # MMKV 持久化（对齐小程序 storage key 语义）
│   └── i18n/copy.ts           # 全部文案常量（与小程序文案同源对照）
├── app.config.ts              # 按环境注入 API_BASE_URL（develop/trial/release 三档对齐小程序）
└── eas.json
```

关键决策：

- **服务端状态一律 TanStack Query**：轮询用 `refetchInterval`（按 §5.1 的间隔规范封装成 `usePolling(queryFn, ms)`），替代小程序手写 timer；缓存键 = 端点语义。
- **本地存储 MMKV**：key 命名沿用 `jianwo_*` 语义（token/report_id/活跃任务等 11 个），便于两端行为对齐与排障。
- **图片**：`expo-image`（磁盘缓存 + 渐入）；相对路径统一在 `client.ts` 响应拦截层拼 `API_BASE_URL`；示例图打包进 app assets 并强制走 `ExampleImage` 组件（内置角标+弱化，等价 `.example-badge/.example-soft`）。
- **相机/相册**：`expo-image-picker`；三图连续拍摄复用小程序交互（三槽 + 单槽重拍 + 拍摄帮助）。
- **触感**：`expo-haptics`，方案切换/勾选/成功态轻触感（对齐小程序 `vibrateShort`）。

### 9.3 页面与导航映射

| 导航 | 内容 |
|---|---|
| Tab：首页/方案/我的 | 与小程序一致，不加第四个 Tab |
| Stack 流程 | capture → profile-setup → analysis → report → plan/[id] → checklist → feedback（对应原型 02–05 步标记） |
| 推入页 | scene（4 场景 Brief）、hair/outfit/purchase、today、wardrobe、advisor、lab、settings、share/web-view |

「方案」Tab 的三方案轮播用 `react-native-pager-view` + `selectThumb` 联动（对齐 plans 页交互）；对比切换（原本/方案）用 §4.4 `compare-toggle`。

### 9.4 动效实现映射

| 原型/小程序 | 手机端实现 |
|---|---|
| FlowStack spring 转场 + 边缘右滑 | `react-native-screens` native stack（`gestureEnabled`）+ 自定义 `cardStyleInterpolator` 按 §4.3 spring 参数；返回手势阈值 92px/0.45 |
| BottomSheet spring | `@gorhom/bottom-sheet`（自定义 spring 配置 500/43/0.9、250/30/1.05） |
| 扫描线 + 焦点脉冲 | Reanimated `withRepeat(withSequence(...))`，辉光用 `expo-linear-gradient` + shadow |
| fade-up 入场 | `Animated.View entering={FadeInDown.delay(n*80).duration(520)}` |
| shimmer 骨架 | Reanimated 循环 translateX |
| check-pop | `withSpring`（对齐 .25s 弹性） |
| `prefers-reduced-motion` | `AccessibilityInfo.isReduceMotionEnabled` → 动效常量置 0 |

### 9.5 登录与身份（二期关键路径）

1. 首启引导：一屏说明价值 + 隐私承诺（照片只对你可见、可随时删除），然后登录。
2. 登录方式：**手机号验证码**（主路径，两端通用）；iOS 追加 **Apple 登录**（提供第三方登录时 App Store 强制）；Android 追加 **微信 App OAuth**（已装微信时最顺）。
3. **账号打通**：任一方式登录后映射同一 `user_id`（§8.4-1）→ 手机端直接可见小程序里建过的档案、报告、衣橱；小程序侧无需改动。设置页提供「账号与端」展示（`GET /v1/me`）与「退出登录」（`DELETE /v1/auth/sessions/current`）。
4. 「删除我的数据」在手机端设置页同样可达（复用 `DELETE /v1/me/data`），文案与小程序一致。

### 9.6 推送与提醒（P1，服务端 §8.4-6）

- iOS：APNs（Expo Notifications 免费通道）；Android：一期**不做**厂商通道（成本高、到达率承诺难），仅前台内提醒 + 清单页本地通知（`expo-notifications` 本地通知实现「晚上提醒我反馈」）；厂商通道作为 M3 后评估项。
- 推送场景克制（符合产品「不制造焦虑」）：分析/生成完成、用户自设的清单提醒。默认全关，引导页不强推授权。

### 9.7 发布与合规清单

| 项 | 说明 |
|---|---|
| 软件著作权 | 国内安卓应用市场上架前置，尽早申请（名称：怎么打扮） |
| ICP 备案 + App 备案 | 工信部 App 备案为上架硬要求；域名沿用现有已备案主体 |
| 隐私政策与权限 | 相机/相册/通知逐项用途说明；照片属敏感个人信息，上传前单独同意（沿用小程序既有告知文案）；《个人信息保护法》删除权由「删除我的数据」承接 |
| AI 合规 | AI 生成图显式标识（§1.3-2）；使用已备案大模型 API；深度合成类功能（发型/妆容/换装预览）全部带标识与「效果示例」口径 |
| 应用市场 | iOS App Store（审核备注：AI 输出说明 + UGC 无社区）；安卓：华为/小米/OPPO/vivo/应用宝按各家材料清单（软著、备案号、隐私政策 URL、测试账号） |
| 版本策略 | `app.config.ts` 三环境对齐小程序 develop/trial/release；JS 层修复走 EAS Update，原生层变更走商店审核 |

---

## 10. 质量与交付

### 10.1 测试策略

| 层 | 现状 | 二期 |
|---|---|---|
| 服务端单测/集成 | `go test ./...`（handler + repository 集成） | 新增 auth/sms/apple/profile 用例；`user_identities` 合并逻辑重点覆盖 |
| 端到端 | `server/scripts/e2e.sh`（17 步含越权与删除） | 增补：sms/apple 登录→同手机号合并→删除完整性 |
| 小程序静态 | `miniapp/scripts/validate.mjs` | 增加 api.js ↔ openapi.yaml 一致性检查（§5.3） |
| 小程序编译 | `miniprogram-ci.getCompiledResult` | 保持，作为发布前门禁 |
| 手机端 | — | Jest（utils/hooks）+ Detox 或 Maestro 冒烟（登录→建档示例体验→报告→方案→反馈 5 步）；每次发版必跑 |
| 视觉回归 | `design-qa.md` 人工对照截图 | 小程序保持人工流程；手机端用 Maestro 截图入 `qa/mobile/` |

### 10.2 验证命令（CI 门禁）

```bash
node miniapp/scripts/validate.mjs
npm run check:runtime          # 原型运行时哈希（涉及 src/ 时）
npm run build && npm run test:sites
cd server && go test ./... && go vet ./... && cd ..
bash server/scripts/e2e.sh     # 需本地 compose 起服务
# 手机端（二期加入）
cd mobile && npm run lint && npm run typecheck && npx maestro test .maestro/smoke.yaml
```

### 10.3 里程碑（估算为单人全职投入的人日带宽，供排期参考；兼职按 50% 折算）

| 里程碑 | 内容 | 验收标准 | 估算 |
|---|---|---|---|
| **M0 一期收口** | G1–G4、G7 差距修复；`contracts/openapi.yaml`；token 统一；队列可观测；`user_identities` 迁移与 wechat 回填 | e2e 扩展全绿；design-qa 一轮 passed；补充资料步骤可跳过可填写且报告个性化生效；plan 页发型师卡可保存到相册 | 8–12 人日 |
| **M1 手机端骨架** | Expo 工程 + 设计系统 RN 化 + 手机号/Apple 登录 + **主闭环全链**（建档→分析→报告→方案→清单→反馈） | 真机（iOS+安卓各一台）跑通主闭环；冒烟测试入 CI；崩溃率 <0.3% | 15–20 人日 |
| **M2 功能对齐 + 发布** | 复访四闭环 + 三工具 + 分享；推送（iOS）；G6；双端商店提审与备案材料 | 功能清单逐项对齐小程序（§5.2 全端点可用）；两店过审上架；「删除我的数据」端到端验证 | 15–20 人日 |
| **M3 体验实验室** | 妆容/换装入口（以身份一致性评测达标为前置）、AR/3D 试点 | `docs/ai-evaluation.md` 门槛全绿才开灰度；未达标则保持关闭并公示原因 | 评测驱动，不设固定工期 |

顺序建议：M0 立即（小程序线上收益直接）；M1/M2 串行为主、登录与设计系统可并行；M3 独立分支不阻塞发布。

### 10.4 风险与开放问题

| 风险 | 应对 |
|---|---|
| AI 图片生成成本失控 | 路由 `max_cost_cny` 守门已内建；上线后按 `InvocationMeta.estimated_cost_cny` 周报；必要时对生成类加每日用户配额 |
| 换装/妆容身份一致性不达标 | 已定义评测门槛与一票否决；入口保持关闭（M3 前置） |
| 国内安卓推送与上架碎片化 | 一期只做 iOS 推送 + 安卓本地通知；上架先华为/小米两家，其余后置 |
| 单人带宽 | 里程碑按可独立发布的切片切分；M0 与 M1 可交叠 |
| 双端文案/行为漂移 | `copy.ts` 与小程序文案对照表；validate.mjs 契约检查；design-qa 双端各留档 |

开放问题（需产品决策）：① 手机端是否做 iPad/横屏（建议明确不支持，锁定竖屏）；② 推送是否接入订阅消息同步触达小程序用户（建议 M2 后评估）；③ 验证码短信签名主体与备案主体是否一致（M1 前确认，否则卡登录）。

---

## 附录 A：环境变量增量（在 technical-design 基础上）

| 变量 | 默认 | 说明 |
|---|---|---|
| `SMS_PROVIDER` | `console` | `console` / `aliyun`；生产必须 `aliyun` |
| `ALIYUN_SMS_*`（sign/TemplateCode/AccessKey 复用 ALIYUN_API_KEY） | — | 阿里云短信 |
| `SMS_RATE_PER_PHONE_PER_HOUR` | `5` | 验证码频控 |
| `APPLE_BUNDLE_ID` | — | Apple 登录 audience 校验 |
| `WECHAT_APP_ID` / `WECHAT_APP_SECRET`（开放平台） | — | App 微信 OAuth（与小程序 secret 分开） |
| `APNS_KEY_ID` / `APNS_TEAM_ID` / `APNS_P8`（或文件路径） / `APNS_BUNDLE_ID` | — | iOS 推送；未配置则推送能力关闭 |
| `ANALYSIS_POLL_MS` | `700` | worker 轮询周期 |

## 附录 B：客户端本地存储 key（两端对齐语义）

`jianwo_token / jianwo_report_id / jianwo_plan_id / jianwo_saved_plan_id / jianwo_saved_hair / jianwo_saved_product / jianwo_active_analysis_id / jianwo_active_plan_generation / jianwo_active_hair_preview / jianwo_plans_scene / jianwo_scene_brief / jianwo_advisor_conversation_id / jianwo_city`；「删除我的数据」时全部清除（token 除外，随登出处理）。

## 附录 C：埋点事件（现有 + 规划）

现有：`page_view`、`today_plan_generate`、`today_plan_activate`、`today_feedback`、`share_card_create`、`wardrobe_item_add`、`wardrobe_outfit_generate`、`wardrobe_outfit_wear`、`advisor_message_send`、`advisor_action_apply`。

规划（M1/M2）：`profile_setup_done`（G1 生效率）、`salon_card_save`（G2 使用率）、`app_login_success`（按 provider 分桶）、`app_main_loop_complete`（手机端首闭环）、`push_opt_in`。命名沿用 `^[a-z][a-z0-9_]{1,63}$`，payload ≤4KB。
