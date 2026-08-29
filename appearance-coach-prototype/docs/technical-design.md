# 「见我」第一阶段技术方案

## 1. 目标与边界

第一阶段交付一个可运行的微信小程序闭环：

1. 首页选择“开始形象分析”或场景快捷入口。
2. 上传正脸、90° 侧脸、正面全身三张照片。
3. 创建异步分析任务并展示进度。
4. 查看当前形象报告、可提升 Tag、优先建议。
5. 对比三套本人方案，查看完整发型/妆容/穿搭建议。
6. 已有档案时，提交场景化 Brief（如面试形式、婚礼身份、约会活动或当日环境），生成该场合专属的三套方案。
7. 生成并勾选改造清单，保存方案并提交反馈。
8. 用户可删除照片和分析数据。
9. 回访用户可获得今日造型、复用轻量衣橱、继续询问私人顾问并分享主动选择的方案。

本仓库内的 Demo Provider 会生成稳定、可复现的分析结果，保证没有第三方模型凭据时也能端到端运行；OpenAI Provider 已实现三张用户照片的真实结构化视觉分析。虚拟试妆/试衣与对象存储继续通过接口替换，不侵入业务层。

## 2. 技术选型

### 2.1 前端：原生微信小程序

- 原生 WXML/WXSS/JavaScript：第一阶段只面向微信，原生方案包体更小、相机/相册/隐私接口最直接，避免 Taro/uni-app 的运行时与兼容成本。
- JavaScript + JSDoc：当前仓库无需额外编译链即可在微信开发者工具运行；API、状态和页面参数均用结构化注释约束。团队扩大后可平滑迁移 TypeScript。
- 自定义组件：`app-header`、`primary-button`、`bottom-nav` 固化设计系统与交互规则。
- 服务层：页面只调用 `services/api.js`，不直接拼接请求；开发环境可切换本地 Demo 数据。
- 状态：第一阶段用 `App.globalData` 保存会话和当前分析上下文，持久状态写入 `wx.setStorageSync`。没有引入全局状态库，避免小规模流程过度设计。

未选择 Taro 的原因：它更适合确定要同时发布 H5/支付宝/抖音的团队；当前关键风险是图像上传、微信审核、隐私与分析闭环，而不是跨端。

### 2.2 后端：Go 1.24 模块化单体

- 标准库 `net/http`：Go 1.22+ 已支持方法路由，减少 Web 框架依赖。
- `pgx/v5`：PostgreSQL 原生驱动、连接池与事务支持成熟。
- 模块化单体：`httpapi -> service -> repository/provider/storage`，清晰但不提前拆微服务。
- API 进程内可启动 Worker；生产可使用同一镜像的 `worker` 命令单独横向扩容。
- Session Token 使用随机 256 位令牌，数据库仅保存 SHA-256 摘要；避免在第一阶段引入 JWT 吊销复杂度。

未选择 Gin/GORM 的原因：接口规模不大，标准库路由和显式 SQL 更容易审计数据权限、事务和查询性能。

### 2.3 数据库：PostgreSQL 16

- 核心关系使用规范化表：用户、照片、分析、报告、方案、清单、反馈。
- 模型原始输出和可变特征使用 `jsonb`，避免每次模型升级都改表。
- 分析任务采用 PostgreSQL `FOR UPDATE SKIP LOCKED`，第一阶段无需 Redis/RabbitMQ。
- 通过 `user_id` 贯穿所有业务表；Repository 的每次读写都同时校验资源 ID 和用户 ID，防止越权。

### 2.4 图片存储

- 本地：`LocalStorage` 将开发上传写入 `server/data/uploads`。
- 生产：实现同一 `ObjectStorage` 接口接入腾讯云 COS/阿里云 OSS；数据库只保存对象 Key，不保存大二进制。
- 用户照片默认私有；生产通过短期签名 URL 返回。Demo 素材为公开静态资源。

## 3. 总体架构

```text
微信小程序
  ├─ 页面 / 组件 / 动效
  └─ services/api.js
          │ HTTPS + Bearer Session
          ▼
Go API
  ├─ httpapi：路由、鉴权、参数与错误映射
  ├─ service：用例、事务边界、资源权限
  ├─ provider：Demo / OpenAI Responses 视觉分析 / 可配置降级
  ├─ storage：Local / COS / OSS
  └─ repository：PostgreSQL
          │
          ▼
PostgreSQL ── analysis_jobs ── Worker ── provider
```

部署首选：一个 API 实例 + 一个 Worker 实例 + 托管 PostgreSQL + 对象存储。用户量较小时 API 与 Worker 可以同进程。

## 4. 领域模型

- `users`：微信主体。生产以 `openid` 唯一标识。
- `user_sessions`：可撤销登录会话，只保存 Token 摘要。
- `profiles`：身高、职业、预算、偏好等可复用档案。
- `media_assets`：三类照片与存储 Key、生命周期。
- `analyses`：一次分析快照，含状态、进度、失败原因。
- `analysis_jobs`：异步队列、重试次数、锁与下次执行时间。
- `reports` / `report_findings`：当前印象、可提升点和首要建议。
- `plans` / `plan_steps`：基础方案与按场合生成的三套方案，以及发型、妆容、穿搭拆解。方案按 `report_id + scene` 分组，记录生成时的轻量场合 Brief。
- `checklist_items`：用户执行清单与完成状态。
- `feedback`：真实使用后的感受，供下一次推荐调整。
- `tool_results`：发型预览、穿搭诊断、购买判断的结构化结果和收藏状态；可关联形象档案与上传素材。
- `today_plans`：按用户和日期唯一保存当天上下文、三项执行步骤、采用状态与反馈。
- `share_cards`：保存不可变方案快照、公开随机 Token、有效期与撤销状态；公开读取不访问用户实时档案。
- `wardrobe_items` / `wardrobe_outfits`：轻量常穿单品、组合快照与穿着次数，不承担商城或完整衣橱管理。
- `advisor_conversations` / `advisor_messages` / `advisor_actions`：上下文对话、结构化动作与应用状态。
- `product_events`：统一产品事件；Payload 仅接收 4 KB 内 JSON 对象，隐私删除时按用户清理。

删除策略：用户主动删除时在一个事务中删除业务数据，并异步清理对象存储；审计日志只保留不可反推照片的操作元数据。

## 5. API 契约

所有业务接口以 `/v1` 开头，响应为 JSON；需要鉴权的接口使用 `Authorization: Bearer <token>`。

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/v1/auth/dev` | 本地开发登录 |
| POST | `/v1/auth/wechat` | 微信 `wx.login` code 换会话（生产适配点） |
| POST | `/v1/media` | 上传一张照片，multipart 字段 `file`、`kind` |
| POST | `/v1/analyses` | 创建分析，提交三个 media id 与场景 |
| GET | `/v1/analyses/{id}` | 轮询进度；完成后返回 report id |
| GET | `/v1/reports/{id}` | 获取报告与 Findings |
| GET | `/v1/reports/{id}/plans?scene={scene}` | 获取基础方案或指定场合的三套方案摘要；不传 `scene` 时返回基础方案 |
| POST | `/v1/reports/{id}/scene-plans` | 复用已有档案，提交 `{scene,answers}` 场景化 Brief，并生成/更新该场合的三套方案 |
| GET | `/v1/plans/{id}` | 获取完整方案与步骤 |
| POST | `/v1/plans/{id}/select` | 选择并生成清单 |
| GET | `/v1/plans/{id}/checklist` | 获取执行清单 |
| PATCH | `/v1/checklist/{itemId}` | 更新完成状态 |
| POST | `/v1/feedback` | 提交使用反馈 |
| POST | `/v1/tools/run` | 运行发型预览、穿搭诊断或购买判断 |
| POST | `/v1/tools/{id}/save` | 将工具结果加入用户方案 |
| POST | `/v1/hair-previews` | 创建本人发型预览异步任务 |
| GET | `/v1/hair-previews` | 获取已保存的本人发型预览 |
| GET | `/v1/hair-previews/{id}` | 轮询生成进度与获取原图/效果图 |
| POST | `/v1/hair-previews/{id}/save` | 保存本人发型预览 |
| GET | `/v1/today/context` | 获取 Weather Provider 归一化后的日期、城市、天气与日程上下文 |
| GET | `/v1/today/plans/current` | 获取当天方案；尚未生成时返回 `data: null` |
| POST | `/v1/today/plans` | 生成或刷新当天发型、妆造与穿搭方案 |
| POST | `/v1/today/plans/{id}/activate` | 加入今日执行清单 |
| POST | `/v1/today/plans/{id}/feedback` | 保存当天反馈 |
| POST | `/v1/share-cards` | 为今日方案或长期方案创建不可变分享快照 |
| GET | `/v1/share/{token}` | 公开读取有效分享卡 |
| POST | `/v1/share-cards/{id}/revoke` | 撤销分享，公开 Token 立即失效 |
| GET/POST/DELETE | `/v1/wardrobe/items` | 查询、录入或移除衣橱 Lite 单品 |
| POST | `/v1/wardrobe/outfits` | 按当天上下文生成现有衣橱组合 |
| POST | `/v1/wardrobe/outfits/{id}/wear` | 记录穿着并返回完整水合单品 |
| POST | `/v1/advisor/messages` | 结合报告、当天方案和衣橱生成顾问回复与动作 |
| GET | `/v1/advisor/conversations/{id}/messages` | 获取本人顾问会话历史 |
| POST | `/v1/advisor/actions/{id}/apply` | 应用顾问动作 |
| POST | `/v1/events` | 写入受约束的产品事件 |
| DELETE | `/v1/me/data` | 删除用户照片和分析数据 |

统一错误：

```json
{"error":{"code":"validation_error","message":"请上传三张照片","request_id":"..."}}
```

## 6. 异步分析与 Provider

1. `POST /analyses` 在事务内写入 `analyses` 和 `analysis_jobs`。
2. Worker 使用 `FOR UPDATE SKIP LOCKED` 领取任务并更新进度。
3. 媒体加载器依据数据库对象 Key 从存储读取三张图片，并限制每张输入大小；Provider 收到图片字节与档案快照，返回版本化结构 `AnalysisOutputV1`。
4. Service 在一个事务中写入报告、Findings、三套方案及步骤。
5. 失败任务指数退避，最多 3 次；永久失败写入安全的用户可读错误，不暴露上游响应。
6. OpenAI Provider 通过 Responses API 提交三张 base64 图片，使用 `store: false` 和严格 JSON Schema；响应返回后继续做集合数量、枚举、锚点区间、文本长度和敏感措辞校验，杜绝医疗诊断、身材羞辱和“颜值分”。
7. `AI_PROVIDER=demo|openai` 控制模式；`AI_FALLBACK_TO_DEMO` 控制上游失败时是否降级。演示环境可开启回退，生产评测和正式分析默认应关闭，以免示例内容被误认为个性化结论。
8. `/healthz` 暴露当前 Provider 与回退开关；每份报告保存 `provider_version`，用于灰度、回放和问题定位，绝不在日志或健康接口中暴露密钥。

当前真实 Provider 在一次多模态请求中生成受约束的结构化特征与建议。数据量增加后可拆成两阶段流水线：视觉测量模型生成结构化特征，LLM 仅根据特征和规则库生成文案；虚拟预览使用独立图像模型并显式标记“AI 效果预览”。当前静态方案图只作为交互示例，不应声明为用户本人生成结果。

### 6.1 本人发型预览

1. 用户上传一张正脸照并选择 `sharp / warm / natural` 三个受控方向之一。
2. API 创建 `hair_previews` 任务后立即返回；独立 Worker 领取任务，避免图像生成阻塞报告分析。
3. OpenAI 模式调用 GPT Image 2 的 `/images/edits`，使用高保真输入、竖版尺寸和身份保持提示，只允许改变发型。
4. 生成结果必须是 20 MB 以内的 PNG；服务端验证文件签名后写入对象存储，数据库保存对象 Key、Provider 版本和任务状态。
5. 上游失败最多重试两次。若配置 Demo 回退，结果必须通过 `provider_version=demo-hair-v1` 在 UI 标为“效果示例”，不可标为本人生成。
6. 原图/生成图跟随用户数据删除；生产对象存储使用短期签名 URL。

### 6.2 穿搭诊断

1. 用户上传一张正面全身穿搭照并选择日常、面试或约会场景。
2. Service 在调用 Provider 前同时校验照片 UUID、用户归属和 `outfit` 类型，防止跨用户图片引用。
3. OpenAI 模式通过 Responses API 提交单张私有 data URL，使用 `store: false` 和严格 JSON Schema。
4. 输出固定为三处可见穿搭标注、一个最高优先级建议和三个执行标签；类别、语气、图片锚点及文本安全均由服务端二次校验。
5. `provider_version` 写入结果 Payload。Demo 降级在 UI 标为“效果示例”，真实响应标为“AI 真实诊断”。
6. 建议默认优先卷袖、换内搭、调整腰线或配色等低成本动作，不引导用户购买整套新品。

### 6.3 今日上下文与顾问 Grounding

1. `WeatherProvider.Current` 只返回城市、天气和温度三个稳定字段；服务层补充日期、工作日/休息日和日程，供应商响应不会进入领域模型。
2. Demo Provider 用于开发与自动化；生产实现已接高德 Web 服务天气实况，并在 Provider 内处理超时、密钥保护和供应商错误映射。
3. 顾问发送消息前并行语义上汇总当天方案、关联报告和用户衣橱；任一可选上下文不存在时安全降级，新用户仍可提问。
4. 回复中的操作按钮保存报告、当天方案和衣橱单品 ID 快照，后续接入 LLM 时仍由服务端校验动作白名单和资源归属。
5. 当前关键词回复器是可预测的第一阶段实现，不伪装成开放式智能；升级模型时保持相同的 `AdvisorMessage` / `AdvisorAction` 契约。

## 7. 安全、隐私与合规

- `APP_ENV=production` 是服务端硬门禁：拒绝开发登录、本地/HTTP API 地址、本地文件存储和 Demo 天气。
- 微信登录使用 `wx.login → /v1/auth/wechat → jscode2session`；服务端持久化 OpenID 对应关系，不保存微信 `session_key`，应用 token 仅保存 SHA-256 摘要。
- 用户照片进入私有腾讯云 COS；数据库保存对象 Key，接口按需返回短时 GET 预签名 URL，COS 子账号权限限制到对应环境前缀。
- 今日造型生产上下文使用高德天气实况，微信、COS 与高德密钥均只由部署 Secret 注入，不进入仓库或日志。
- 小程序 API 地址按 `develop / trial / release` 分开，体验版与正式版缺少 HTTPS 域名时直接失败，不回退到开发接口。
- 首次上传前展示用途、保存期限、删除入口和 AI 预览说明。
- 只收集完成建议所需的数据；体重、三围不是第一阶段必填。
- 上传限制：JPEG/PNG/WEBP、单张 10 MB、服务端重新识别 MIME、随机对象 Key。
- 照片 URL 不写入日志；日志对 Token、openid 和路径脱敏。
- API 限流建议：登录 10/min/IP，上传 20/hour/user，分析 5/day/user。
- 生产启用 TLS、数据库与对象存储加密、备份 PITR、最小权限 IAM。
- 报告措辞强调“可提升点”和可执行建议，不输出颜值/身材打分。

## 8. 视觉与交互原则

- 视觉 Token：暖珍珠白 `#F8F5F0`、石墨黑 `#252725`、苔藓绿 `#587344`、鼠尾草浅色 `#EEF2E9`。
- 8pt 间距体系，主按钮最小高度 52px，触控热区不小于 44px。
- 中文标题用系统宋体，正文用系统无衬线，最多两套字体。
- 动效只用于反馈和空间关系：页面淡入上移、扫描线、进度呼吸、卡片选择缩放、清单勾选弹性反馈。
- 遵循 `prefers-reduced-motion` 思路：小程序设置中可关闭非必要动画。
- 图片始终标注“当前/方案”，避免把 AI 预览误解为真实效果承诺。
- 五张已确认视觉稿继续约束暖白、石墨、苔绿、真实人物图片与克制卡片语言；首页依据最新产品决策收敛为原生 APP 工作台，减少落地页式大标题、口号和长纵向营销节奏。
- 首页采用新用户/回访用户双状态，并固定 `首页 / 方案 / 我的` 三项底部导航；快捷工具为发型预览、穿搭诊断和购买判断，最近方案承担连续使用入口。
- “开始形象分析”与场景入口是两条并列入口。场景入口保持四项单页选择和统一交互，但内容按场景变化：面试看面试形式与准备范围，婚礼看身份与着装分寸，约会看活动与时段，日常看行程与环境；首次使用再补齐三图档案，已有报告时直接复用档案，不重复采集长表单。
- 场景方案再生成是同步的轻量用例：输入仅为场合 Brief，不再次上传照片或运行视觉分析；同一报告同一场景始终保留最新的一组三套方案，重新生成会替换该场景旧方案及其未完成清单。每个场景可独立选定一套方案，不会取消其他场景的选择。
- AR、3D 与试衣能力集中到体验实验室，以功能状态和 AI 预览标识管理用户预期；体重和三围始终为按需选填，不阻塞基础闭环。

## 9. 可观测性与指标

- 每个响应返回 `X-Request-ID`；日志统一包含 request_id、user_id_hash、route、latency、status。
- 指标：上传成功率、分析完成率/P95 耗时、报告到方案点击率、方案选择率、清单完成率、删除请求成功率。
- 告警：分析失败率 > 5%、队列最老任务 > 5 分钟、数据库连接池饱和、对象存储 5xx。
- Provider 指标按 `provider_version` 分桶：成功率、拒绝率、Schema 拒收率、回退率、请求耗时与单次成本；生产回退率 > 0 即应告警并抽查报告来源。

## 10. 测试与发布

- Go：Service/Provider 单元测试、HTTP Handler 测试、PostgreSQL Repository 集成测试。
- 小程序：纯函数与 API 映射 Node 测试，`miniprogram-ci` 官方编译器验证，微信开发者工具进行页面渲染和真机验证。
- 当前仓库提供 `miniapp/scripts/validate.mjs` 做页面、组件、本地素材和 WXML 受限语法的静态检查；正式发布前仍需在目标基础库和至少一台 iOS/Android 真机完成相机、相册与安全区回归。
- CI：`go test ./...`、`go vet ./...`、迁移校验、小程序脚本测试、敏感信息扫描。
- 发布：迁移先行且向后兼容；API/Worker 滚动发布；模型输出带 `provider_version` 便于回放。

## 11. 后续演进

当单日分析任务持续超过单库队列能力，才将 `analysis_jobs` 迁移到托管消息队列；当实时预览和推荐搜索变为独立负载，再拆分媒体、分析和推荐服务。第一阶段保持模块化单体能显著降低运维和数据一致性成本。
