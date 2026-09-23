# 3D 形象 Lite（环绕视频 + 转盘静帧）

> 状态：待评审  
> 日期：2026-09-12  
> 范围：体验实验室「3D 形象 Lite」；服务端全身表示资源；小程序 `BodyViewer`  
> 前提：加在当前 monorepo 架构上（统一 `tasks` + 能力路由 + `@zsm/core` 媒体契约），不依赖质量核心重构是否落地

## 1. 结论

第一版用伪 3D 交付实验室的「哇塞」：服务端生成一条环绕短视频，再抽出 16 张 JPEG 做转盘。小程序先播一圈视频，再让用户拖静帧停住、和原全身照对比。

对外只暴露一种资源 `BodyPresentation`。`representation` 取值 `orbit` 或 `mesh`；v1 只写 `orbit`，`mesh` 恒为 `null`。以后参数人体（方案 B）填同一字段，小程序仍播烘好的 frames，不必改主路径。

不做高斯 / 多视重建（方案 C）。不做小程序 WebGL。不做量体。不把 3D 挂到首页或方案大图。

生成策略锁定为 **视频先出、再抽帧**：一次模型调用保证时间轴上是同一个人；16 张分开出图会漂脸，成本也高。`yaw` 按时间均分写入（0、22.5、…），不承诺光学角度精确。

## 2. 目标与非目标

### 2.1 目标

1. 实验室能对 Demo 或本人照片转一圈、停在某一帧、和原全身照对比。
2. 立体感来自环绕视频；细看和对比来自抽帧，不靠视频 seek。
3. AI 结果按红线标识：`provider_version` 以 `demo` 开头标「效果示例」，否则标「AI 风格预览」。
4. 客户端只认 `BodyPresentation`，不根据 URL 后缀或厂商名推断能力。
5. 新增任务类型只注册 handler，不改认领循环。
6. 业务代码只依赖能力名 `body_orbit`，不出现厂商或模型名。

### 2.2 非目标

- 不重建网格、不估计三围、不输出尺寸建议。
- 不上半身试衣（实验室另一张卡，仍排队）。
- 不在小程序或本期 Expo 里渲染 `mesh`。
- 不做双视频同步对比，不做视频上的左右揭示滑杆。
- 不新增采集步骤；用档案里已有的正面全身 + 正脸。
- 不把 3D 做成第四个 Tab，不改 `首页 / 方案 / 我的`。
- 不引入消息队列、独立媒体服务或客户端 Three.js。
- 不为质量核心重构做双写或兼容层；若那份设计先落地，本资源再映射为一条公开 Operation。

## 3. 成功标准

- 路由未配置 `body_orbit` 时，实验室 3D 卡保持候补，不出现空播放器或内置模特冒充本人。
- 路由已配置时：无正脸或全身档案 → 空态，动作是去拍摄；有档案 → 可创建任务。实验室用 `GET /v1/body-presentations/status` 判断能力，不靠先 POST 再猜。
- Demo 结果角标为「效果示例」；非 demo 的本人结果角标为「AI 风格预览」。抽检错配次数为 0。
- 成功结果必须有一条可经 `lookVideo()` 播放的 MP4。抽帧 ≥ 8 张才出现对比滑杆；1–7 张或抽帧失败仍算成功，只播视频。
- 视频失败（无字节 / 非 MP4）整单失败，不回退示例图。
- 页面隐藏即停轮询；失败满 5 次进错误态，动作是重试或返回。
- 包内与接口均无 WebP 视频或 WebP 帧。
- 越权访问他人 `presentation_id` 返回 404。
- 不出现颜值、身材分、测量数字。

## 4. 用户流程

入口只有 `packages/tools/pages/lab` 的「3D 形象 Lite」。发型 AR、上半身试衣本轮不改。

实验室进入时拉 `GET /v1/body-presentations/status`，再读当前档案的正脸 / 全身 media id（与拍摄页同一对，来自当前分析或报告，不新采集）。

```text
实验室
  → status.available = false：候补（现状）
  → available、缺正脸或全身：空态「先完善形象档案」→ 拍摄
  → available、有档案：
       有 active → 进度
       否则最新一条是失败且比 completed 新 → 错误态（可「看上一圈」若 completed 存在）
       否则有 completed → BodyViewer
       否则主按钮「生成 3D 形象」
            → 202 + body_orbit 任务
            → 轮询（900ms，useTaskPolling，onHide 停）
            → 成功：BodyViewer
            → 失败：错误态（重试 / 返回）
```

`BodyViewer` 交互锁定如下：

1. 有可播 `orbit.video_url`：进入后无声自动播一圈，不循环。播完切到转盘。用户中途拖画面则立刻切转盘。v1 成功路径总有视频。
2. `lookVideo` 为空但可见帧 ≥ 8：直接转盘（视频 URL 被拒，或日后只烘了 frames）。
3. 转盘：左右拖切换相邻帧，首尾相接。松手停在当前帧，不惯性甩过超过 1 帧。
4. 对比：仅当 `frames.length >= 8`。底层 `userImage(全身照)`，上层 `lookImage(frames[0])`，沿用现有 `CompareSlider`。`frames[0]` 定义为抽帧序列的第一张，当作「最接近正面」。
5. 只有视频、不够 8 帧：隐藏对比，不展示坏滑杆。

文案（写入 `packages/core/src/copy/zh.ts`，禁止页面内硬编码）：

- 标题：`3D 形象 Lite`
- 说明：`表达比例和穿搭轮廓，不承诺精确测量。`
- 生成：`生成 3D 形象`
- 空态：`先拍正脸和正面全身，才能转起来看。`
- 视频失败：`这一圈没生成成功，再试一次或先返回。`
- 无对比：`对比需要更完整的静帧，先转着看。`

## 5. 资源模型

### 5.1 领域对象

`BodyPresentation` 是用户的一次全身立体预览，不是网格文件，也不是任务本身。

```text
BodyPresentation
  id
  body_media_id          // media_assets.kind = body，必须属该用户
  face_media_id          // media_assets.kind = face，必须属该用户
  representation         // "orbit" | "mesh"；v1 只写 orbit
  orbit
    video_url            // 可空
    duration_ms          // 视频存在时必填
    frames[]             // { yaw: number, url: string }  0..n-1
  mesh                   // v1 恒为 null；JSON 字段保留
  provider_version
  status / progress / stage / error   // 投影自 body_orbit 任务，表内不存队列态
  task                   // 进行中或最近一次任务的 TaskView
  created_at
  updated_at
```

`yaw` 计算：`yaw_i = round(i * 360 / n)`，`n` 为实际抽帧数。这是时间均分标签，不是相机标定值。客户端按数组下标转，不按 yaw 做插值。

`mesh` 预留形状（本轮不填充、不校验内部字段）：

```text
mesh: {
  format: "glb",
  url: string,
  texture_url?: string
} | null
```

v1 API 仍输出 `"mesh": null`，好让 `@zsm/core` 类型一次到位。禁止在 v1 增加 `splat` / `nerf` 字段。

### 5.2 表

新迁移 `021_body_presentations.sql`（只向前）：

```text
body_presentations
  id                 uuid PK
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
  body_media_id      uuid NOT NULL REFERENCES media_assets(id)
  face_media_id      uuid NOT NULL REFERENCES media_assets(id)
  representation     text NOT NULL DEFAULT 'orbit'
                     CHECK (representation IN ('orbit', 'mesh'))
  video_url          text NOT NULL DEFAULT ''
  video_storage_key  text NOT NULL DEFAULT ''
  duration_ms        int  NOT NULL DEFAULT 0
  frames             jsonb NOT NULL DEFAULT '[]'
  mesh               jsonb
  provider_version   text NOT NULL DEFAULT ''
  created_at         timestamptz NOT NULL DEFAULT now()
  updated_at         timestamptz NOT NULL DEFAULT now()
```

索引：`(user_id, created_at DESC)`。

`frames` 元素：`{"yaw":0,"url":"/uploads/...","storage_key":"..."}`。URL 给客户端，key 给删除与存储回收。

删除用户数据时：删行，并删除 `video_storage_key` 与每帧 `storage_key`。越权读按 `id + user_id` 过滤，未命中 404。

队列态只活在 `tasks`。任务 payload：`{"presentation_id":"<uuid>"}`。新类型常量：`TaskTypeBodyOrbit = "body_orbit"`。

## 6. API

与发型预览同形，走现有 Envelope + `task`。

| 方法 | 路径 | 行为 |
|---|---|---|
| POST | `/v1/body-presentations` | 校验两张媒体归属与 kind；入账 look 额度；插入行；入队 `body_orbit`；202 返回资源 + `task` |
| GET | `/v1/body-presentations/{id}` | 投影该行最新任务状态 |
| GET | `/v1/body-presentations/status` | 实验室启动用：能力开关 + 进行中 + 最近成功 + 最近失败 |

请求体：

```text
{ "body_media_id": "...", "face_media_id": "..." }
```

不要 style_id、scene、mesh 参数。v1 不提供保存列表、不提供再生成专用端点：再点「生成」就是再 POST 一条新记录。同一用户同时只允许一条 queued/processing 的 `body_orbit`（与发型预览一致）；重复 POST 返回已有进行中资源 + 其 task，不重复扣费。

`GET /v1/body-presentations/status` 响应：

```text
{
  available: boolean,          // ai-routing 是否配置了 body_orbit
  active: BodyPresentation | null,
  completed: BodyPresentation | null,  // 最新一条已有可播 MP4 的记录
  failed: BodyPresentation | null      // 比 completed 更新的最新失败；否则 null
}
```

再生成失败时 `completed` 仍指向上一圈成功结果，避免错误态盖掉已能看的预览。

契约写入 `contracts/openapi.yaml`，`packages/core` 端点表与 check-sync 互锁。`TaskType` 联合类型增加 `body_orbit`。

未配置 `body_orbit` 路由时：`status.available = false`；POST 返回 503，`code=capability_unavailable`，不创建行。客户端以 status 为准，不把 POST 当探测。

## 7. 生成管线

### 7.1 分层

```text
httpapi  翻译 HTTP
service  校验媒体、入账、入队、完成写入
handler  注册到现有 ClaimTask 循环
provider 只通过 OrbitGenerator
storage  写 COS / 本地盘
extract  Worker 内 ffmpeg，不是 AI
```

业务只依赖：

```text
type OrbitGenerator interface {
  Generate(ctx, input {Body, Face []byte; BodyMIME, FaceMIME string}) (OrbitOutput, error)
}

type OrbitOutput struct {
  VideoData []byte     // 必须是 MP4 / H.264
  MIMEType  string     // video/mp4
  Duration  time.Duration
  ProviderVersion string
}
```

`body_orbit` 写在 `ai-routing.*.json`。没有该键即能力关闭。新增视频协议适配器时只改 `provider` 与路由文件。

v1 必须带 `DemoOrbitGenerator`：返回仓库内固定短 MP4 夹具（H.264，约 3 秒，长边 ≤ 720）。`ProviderVersion` 以 `demo` 开头（`demo-body-orbit-v1`）。夹具走同一套抽帧，避免 Demo 抄近路跳过 extract。

生产要接真实模型时：只加协议适配器 + 路由主键，不改 handler 主流程。本设计不把具体厂商写进业务包。

### 7.2 Worker 步骤

1. 按 `user_id` 加载 presentation 与两张媒体。缺失或 kind 不对 → 永久失败。
2. 读图，按现有 `editBudget`（长边 1536 / JPEG 85）约束后交给 Generator。
3. Generator 失败 → 可重试（上限 2，与其他生成类一致）。无视频字节或 MIME 不是 `video/mp4` → 永久失败。
4. 存 MP4：`{user_id}/generated/body-orbit/{id}.mp4`。
5. ffmpeg 抽帧：`n = 16`，时间戳 `t_i = duration * i / n`（最后一帧用 `duration * (n-1) / n`，避免撞上文件尾）。输出 JPEG，长边 ≤ 720，质量 80。
6. 抽到 ≥ 8 张：写入 `frames`，任务完成。
7. 抽到 1–7 张或 ffmpeg 失败：保留视频，不写入这些残帧（`frames = []`），任务仍标记完成。日志 WARN。对比关闭。不展示 8 张以下的转盘。
8. 0 张且无视频：失败（正常不会走到，视频已在步骤 3 守住）。

进度文案（写入 copy 或服务端 stage 用中文，与现有任务一致）：

- 12% `正在读取全身和正脸`
- 40% `正在生成环绕预览`
- 72% `正在抽出转盘静帧`
- 95% `正在保存`

### 7.3 ffmpeg

抽帧是本地工具，不进能力路由。

- 生产（`APP_ENV=production`）：进程启动时检查 `ffmpeg` 在 PATH，缺失则拒绝启动（与现有生产门禁同一类）。
- 开发：缺失则抽帧走步骤 7 的降级，API/Worker 仍可跑。
- Docker 构建：server 镜像安装 ffmpeg。compose 的 api 服务即 Worker 宿主时一并带上。
- 测试：`OrbitExtractor` 做成接口，单测注入假抽帧；另备一条用真实夹具 + 本机 ffmpeg 的集成测试，无 ffmpeg 时跳过。

命令语义（实现可微调参数，语义锁定）：对输入 MP4 按上述时间戳各出一张 `mjpeg`/`image2` JPEG，禁止输出 WebP。

### 7.4 视频约束

- 容器：MP4；编码：H.264。其它容器一律永久失败。
- 时长：目标 3.5 秒。接受 2–6 秒；超出仍抽帧，不失败。
- 分辨率：长边 ≤ 720。更大则在抽帧前用 ffmpeg 缩视频后再抽。
- 音频：忽略。客户端永远 muted。

## 8. 客户端

### 8.1 小程序

- 新组件 `apps/miniapp/src/components/body-viewer/`：`styleIsolation: apply-shared`，样式只写 `rpx`。
- 列表 key 用 `yaw` + `url`，不用下标。
- 条件渲染禁止 `{count && <View/>}`。
- 帧图：`lookImage(url)`；无效则该帧不渲染，转盘跳过空帧。空帧过多导致可见帧 < 8 时按「无对比」处理。
- 视频：新增 `lookVideo(url)`（见 §9）。`<Video>` 只在 `lookVideo` 非空时挂载；`muted`、`autoplay`、不显示默认控件条、`object-fit: contain`。
- 轮询：只许 `@zsm/core` 的 `useTaskPolling`，间隔 900ms。
- 媒体真实性：禁止页面私自把失败 URL 换成包内模特图。实验室列表卡上的预览图在未生成前仍用 `exampleImage` + 「风格参考」。

Expo 本期不实现 `BodyViewer`。`@zsm/core` 类型与端点要先加上，避免二次分叉。

### 8.2 实验室页状态

现有三张卡保留。3D 卡：

- `available = false`：候补（已有交互）。
- 有能力未生成：说明 + 主按钮。
- 进行中：进度与 stage。
- 失败：`ErrorState`，重试 POST（进行中去重）；若 `completed` 仍在，提供「看上一圈」回到 BodyViewer，与错误态不同屏。
- 成功：展开 `BodyViewer`，主按钮改为「再生成一圈」（新 POST）。

本地用现有 storage key 风格记 `zsm_active_task_body_orbit`，丢缓存时靠 `status.active` 恢复。

## 9. 真实性、标识与文案

`packages/core/src/media/truth.ts`：

- `lookImage`：继续用于每一帧。WebP / 非法 URL → `''`。
- `lookVideo(value)`：仅接受 `https://`、`http://`（开发）、`wxfile://`、`file://`，且路径或明确类型像 MP4。WebP、WebM、空串 → `''`。不回退任何内置视频。
- `userImage`：对比滑杆底层全身照。
- `exampleImage`：实验室未生成时的卡片封面，必须叠角标 + `.example-soft`。

角标规则（调用方显式选择，组件不猜）：

- `provider_version` 以 `demo` 开头，或源媒体 `storage_key` 以 `demo/` 开头 → 「效果示例」。
- 其余成功生成 → 「AI 风格预览」。
- 未生成的卡片封面 → 「风格参考」。

`isBundledAsset` 不把 `/uploads/` 当内置图。

## 10. 计费与并发

`body_orbit` 算 look 类动作：`authorize(..., domainActionLook, "", 1)`。

`CountActiveTasksByTypes` 把 `body_orbit` 与 `hair_preview` / `plan_look` / `today_look` 算在同一活跃生成上限里。

进行中去重：已有 queued/processing 的 `body_orbit` 时，POST 不扣第二次。

## 11. 错误与降级

| 情况 | 结果 |
|---|---|
| 路由缺失 | 503 `capability_unavailable`，UI 候补 |
| 媒体不存在 / 非本人 / kind 错 | 400，不入队 |
| 已有进行中任务 | 202 返回该条，不扣费 |
| 额度不足 | 与现有 look 同一错误码 |
| Generator 失败 | 重试至上限 2，再失败 |
| 非 MP4 | 永久失败 |
| ffmpeg 失败或 < 8 帧 | 有视频则完成（无对比）；无视频则失败 |
| 越权 ID | 404 |
| 轮询失败 5 次 | 错误态，不展示半截帧 |

错误态与成功内容不同屏。没有「用 natural.jpg 顶上」的隐式回退。

## 12. 方案 B 预留 / 方案 C 禁止

B（参数人体）允许的后续动作，且只有这些：

- 同一张表写入 `representation = mesh` 与 `mesh` JSON。
- Worker 从网格再烘 MP4 + frames，小程序不改渲染器。
- Expo 新增网格渲染分支，仅当 `representation === "mesh" && mesh.url`。

C 禁止：采集绕场视频、Gaussian / NeRF Provider、API 增加 `splat` 字段、小程序原生 3D 插件。

## 13. 测试与发布

提交前仍跑仓库门禁：

```text
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

必须新增的测试：

- Service：媒体 kind/归属、去重不扣费、路由缺失 503、完成写入 frames/video。
- Handler：Demo 夹具走出「存视频 → 抽帧 → 完成」；抽帧失败但有视频 → completed 且 frames 空。
- Extractor：16 个时间戳公式；输出 JPEG。
- Billing：`body_orbit` 计入活跃 look。
- `lookVideo`：拒 WebP/WebM/空。
- OpenAPI ↔ `API_PATHS` check-sync。
- 小程序：`check.mjs` 无裸 `px`、无 WebP 资产。

手动真机：实验室生成（Demo）、播完切转盘、拖一周、对比滑杆、切后台停轮询、无档案空态、无路由候补。

## 14. 目录边界

只改这些树：`apps/server`、`apps/miniapp`、`packages/core`、`contracts/`。根级 `package.json` / `tsconfig.base.json` / `Makefile` / `docker-compose.yml` 仅在为镜像装 ffmpeg 或补门禁脚本时改最小必要行，单独在实现计划里列出。

不改 `appearance-coach-prototype/`。
