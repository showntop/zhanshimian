# 3D 形象 Lite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 体验实验室「3D 形象 Lite」能生成一条环绕 MP4、抽出转盘 JPEG，小程序先播一圈再拖着停、和原全身照对比。

**Architecture:** 复用统一 `tasks` + `ClaimTask`。新资源 `BodyPresentation`，任务类型 `body_orbit`，能力名 `body_orbit`。v1 在路由存在时接 `DemoOrbitGenerator`（夹具 MP4），Worker 用 ffmpeg 抽 16 帧；少于 8 帧丢掉残帧只留视频。小程序 `BodyViewer` 只认该资源，不渲染 mesh。

**Tech Stack:** Go 1.24、PostgreSQL 16、Taro 4 小程序、`@zsm/core`、ffmpeg（Worker 本地抽帧，不进 AI 路由）。

## Global Constraints

- 产品名 uplook；文案只进 `packages/core/src/copy/zh.ts`；不写颜值/身材分/测量数字。
- AI 图标识：`provider_version` 以 `demo` 开头 →「效果示例」；本人生成 →「AI 风格预览」；未生成卡片 →「风格参考」。
- `lookImage` / 新 `lookVideo` 失败返回 `''`，禁止回退包内模特图；包内与接口禁 WebP。
- 小程序样式只写 `rpx`；组件 `styleIsolation: 'apply-shared'`；禁 `{count && <View/>}`；列表 key 不用下标。
- 轮询只用 `useTaskPolling`，间隔 `POLL_INTERVALS.bodyOrbit`（900ms）；页面隐藏即停；失败 5 次进失败态。
- 业务代码不出现厂商/模型名；只依赖能力 `body_orbit`。
- 新任务只注册 handler，不改认领循环结构。
- 迁移只向前：`021_body_presentations.sql`。
- 只改 `apps/server`、`apps/miniapp`、`packages/core`、`contracts/`；根级文件仅 Dockerfile / 生产启动检查。
- 不改 `appearance-coach-prototype/`。生产路由**不要求**配置 `body_orbit`（实验室可选）。
- v1 `mesh` 恒为 JSON `null`；禁止加 `splat` / `nerf` 字段。
- `yaw = 360.0 * i / n`（float64）。n=16 时为 0、22.5、45…；客户端按下标转，不按 yaw 插值。

## File Map

| 文件 | 职责 |
|---|---|
| `packages/core/src/media/truth.ts` | `lookVideo()` |
| `packages/core/tests/truth.test.mjs` | `lookVideo` 契约测试 |
| `packages/core/src/copy/zh.ts` | `LAB_COPY` |
| `packages/core/src/types/index.ts` | `TaskType` + `BodyPresentation` |
| `packages/core/src/hooks/useTaskPolling.ts` | `bodyOrbit: 900` |
| `packages/core/src/api/endpoints.ts` | 路径与端点函数 |
| `packages/core/src/index.ts` | 导出 |
| `contracts/openapi.yaml` | 3 个 path + schema + Task 枚举 |
| `apps/server/internal/media/orbit_extract.go` | 时间戳公式 + ffmpeg 抽帧 |
| `apps/server/internal/domain/domain.go` | 领域类型与 `TaskTypeBodyOrbit` |
| `apps/server/internal/database/migrations/021_body_presentations.sql` | 表 |
| `apps/server/internal/provider/orbit.go` | `OrbitGenerator` 接口 |
| `apps/server/internal/provider/orbit_demo.go` | Demo 夹具 |
| `apps/server/assets/demo/body-orbit.mp4` | 约 3s H.264 夹具 |
| `apps/server/internal/repository/postgres/body_presentations.go` | 仓储 |
| `apps/server/internal/service/body_orbit.go` | 创建 / 读取 / status |
| `apps/server/internal/service/tasks.go` | handler |
| `apps/server/internal/httpapi/body.go` | HTTP |
| `apps/server/config/ai-routing.example.json` | 本地 demo 路由 |
| `apps/server/Dockerfile` | 安装 ffmpeg |
| `apps/miniapp/src/components/body-viewer/` | 播放 + 转盘 |
| `apps/miniapp/src/packages/tools/pages/lab/` | 实验室状态机 |

---

### Task 1: `lookVideo`、文案、类型、轮询间隔

**Files:**
- Modify: `packages/core/src/media/truth.ts`
- Modify: `packages/core/tests/truth.test.mjs`
- Modify: `packages/core/src/copy/zh.ts`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/hooks/useTaskPolling.ts`
- Modify: `packages/core/src/index.ts`

**Interfaces:**
- Consumes: 现有 `isDisplayableImage`
- Produces: `lookVideo(value: unknown): string`；`LAB_COPY`；`TaskType` 含 `'body_orbit'`；`BodyPresentation` / `BodyPresentationStatus` / `CreateBodyPresentationInput`；`POLL_INTERVALS.bodyOrbit === 900`

- [ ] **Step 1: 写失败测试**

在 `packages/core/tests/truth.test.mjs` 追加，并补上 `lookVideo` import：

```js
test('lookVideo: 空/WebP/WebM/非法拒绝，MP4 协议地址通过', () => {
  assert.equal(lookVideo(''), '')
  assert.equal(lookVideo(null), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webp'), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webm'), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webm?v=1'), '')
  assert.equal(lookVideo('not-a-url'), '')
  assert.equal(lookVideo('https://cdn.example.com/orbit.mp4'), 'https://cdn.example.com/orbit.mp4')
  assert.equal(lookVideo('http://127.0.0.1:58000/uploads/u/orbit.mp4'), 'http://127.0.0.1:58000/uploads/u/orbit.mp4')
  assert.equal(lookVideo('wxfile://tmp/orbit.mp4'), 'wxfile://tmp/orbit.mp4')
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `pnpm --filter @zsm/core test`

Expected: FAIL，`lookVideo is not defined` 或 import 失败。

- [ ] **Step 3: 实现最小代码**

`truth.ts` 在 `lookImage` 后增加：

```ts
export function lookVideo(value: unknown): string {
  if (typeof value !== 'string' || !value) return ''
  if (/\.(webp|webm)(\?\S*)?$/i.test(value)) return ''
  if (!isDisplayableImage(value)) return ''
  if (/\.mp4(\?\S*)?$/i.test(value)) return value
  return ''
}
```

`zh.ts` 在 `IMAGE_BADGE_COPY` 后增加（页面禁止硬编码这些句子）：

```ts
export const LAB_COPY = {
  title3d: '3D 形象 Lite',
  desc3d: '表达比例和穿搭轮廓，不承诺精确测量。',
  generate: '生成 3D 形象',
  regenerate: '再生成一圈',
  empty: '先拍正脸和正面全身，才能转起来看。',
  emptyAction: '去拍摄',
  failed: '这一圈没生成成功，再试一次或先返回。',
  retry: '再试一次',
  viewLast: '看上一圈',
  noCompare: '对比需要更完整的静帧，先转着看。',
  badgeAI: 'AI 风格预览',
  waitlist: '已加入候补'
} as const
```

`types/index.ts`：

```ts
export type TaskType = 'analysis' | 'plan_group' | 'plan_look' | 'hair_preview' | 'today_look' | 'body_orbit'

export type BodyRepresentation = 'orbit' | 'mesh'

export interface OrbitFrame {
  yaw: number
  url: string
}

export interface BodyOrbit {
  video_url?: string
  duration_ms?: number
  frames: OrbitFrame[]
}

export interface BodyMesh {
  format: 'glb'
  url: string
  texture_url?: string
}

export interface BodyPresentation {
  id: string
  body_media_id: string
  face_media_id: string
  representation: BodyRepresentation
  orbit: BodyOrbit
  mesh: BodyMesh | null
  provider_version?: string
  status: TaskStatus
  progress: number
  stage: string
  error_message?: string
  task?: Task
  created_at: string
  updated_at: string
}

export interface BodyPresentationStatus {
  available: boolean
  active: BodyPresentation | null
  completed: BodyPresentation | null
  failed: BodyPresentation | null
}

export interface CreateBodyPresentationInput {
  body_media_id: string
  face_media_id: string
}
```

`useTaskPolling.ts` 的 `POLL_INTERVALS` 增加 `bodyOrbit: 900`。

`index.ts`：从 `truth.ts` 导出 `lookVideo`；从 `copy/zh.ts` 导出 `LAB_COPY`。

- [ ] **Step 4: 跑测试确认通过**

Run: `pnpm --filter @zsm/core test && pnpm --filter @zsm/core typecheck`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add packages/core/src/media/truth.ts packages/core/tests/truth.test.mjs \
  packages/core/src/copy/zh.ts packages/core/src/types/index.ts \
  packages/core/src/hooks/useTaskPolling.ts packages/core/src/index.ts
git commit -m "$(cat <<'EOF'
feat: 为 3D 形象 Lite 加上 lookVideo 契约和领域类型。

EOF
)"
```

---

### Task 2: OpenAPI 与 core 端点

**Files:**
- Modify: `contracts/openapi.yaml`
- Modify: `packages/core/src/api/endpoints.ts`
- Test: `node contracts/scripts/check-sync.mjs`

**Interfaces:**
- Consumes: Task 1 的 `BodyPresentation` / `BodyPresentationStatus` / `CreateBodyPresentationInput`
- Produces: `API_PATHS.bodyPresentations = 'POST /v1/body-presentations'`；`bodyPresentation = 'GET /v1/body-presentations/{id}'`；`bodyPresentationStatus = 'GET /v1/body-presentations/status'`；`createApiEndpoints` 上的 `createBodyPresentation` / `getBodyPresentation` / `getBodyPresentationStatus`

- [ ] **Step 1: 先改 `API_PATHS` 并跑 check-sync 看它失败**

在 `API_PATHS` 的 `hairPreviewSave` 后插入：

```ts
  bodyPresentations: 'POST /v1/body-presentations',
  bodyPresentationStatus: 'GET /v1/body-presentations/status',
  bodyPresentation: 'GET /v1/body-presentations/{id}',
```

`ApiEndpoints` 增加：

```ts
  createBodyPresentation(input: CreateBodyPresentationInput): Promise<TaskCreated<BodyPresentation>>
  getBodyPresentation(id: string): Promise<BodyPresentation>
  getBodyPresentationStatus(): Promise<BodyPresentationStatus>
```

`createApiEndpoints` 返回值增加（`createBodyPresentation` 用 `client.requestEnvelope`，与发型预览 202 相同）：

```ts
    createBodyPresentation: (input) =>
      client.requestEnvelope('/v1/body-presentations', { method: 'POST', data: input, timeout: 30000 }),
    getBodyPresentationStatus: () => client.request('/v1/body-presentations/status'),
    getBodyPresentation: (id) => client.request(pathId('/v1/body-presentations', id)),
```

在 `endpoints.ts` 顶部 type import 中加入 `BodyPresentation`、`BodyPresentationStatus`、`CreateBodyPresentationInput`。

- [ ] **Step 2: 跑 check-sync 确认失败**

Run: `node contracts/scripts/check-sync.mjs`

Expected: FAIL，提示 core 多出 `/v1/body-presentations` 与 `/v1/body-presentations/status`。

- [ ] **Step 3: 写 OpenAPI**

在 `contracts/openapi.yaml` 的 `paths:` 里、`/v1/hair-previews/{id}/save` 之后插入三个 path（缩进 2 空格路径、4 空格方法，满足 check-sync 假设）。`POST` 202 的 schema 为 Envelope + `data: BodyPresentation` + `task: TaskRef`。`GET /v1/body-presentations/status` 200 的 `data` 为 `BodyPresentationStatus`。`GET /v1/body-presentations/{id}` 越权/不存在 404。

`components.schemas.TaskRef` 与 `Task` 的 `type.enum` 都加上 `body_orbit`（现有枚举已有 `analysis, hair_preview, plan_look, today_look`，本任务只追加 `body_orbit`，不顺手改 `plan_group`）。

`BodyPresentation` / `BodyOrbit` / `OrbitFrame` / `BodyMesh` / `BodyPresentationStatus` 字段与 Task 1 类型同名同必填：`orbit.frames` 必填数组；`mesh` 可 null；`representation` enum `orbit|mesh`。

- [ ] **Step 4: 再跑 check-sync**

Run: `node contracts/scripts/check-sync.mjs && pnpm --filter @zsm/core typecheck`

Expected: `[check-sync] ✅ 契约与 core 端点完全一致`；typecheck PASS。

- [ ] **Step 5: Commit**

```bash
git add contracts/openapi.yaml packages/core/src/api/endpoints.ts
git commit -m "$(cat <<'EOF'
feat: 增加 body-presentations 契约和客户端端点。

EOF
)"
```

---

### Task 3: 抽帧时间戳与 ffmpeg 抽取器

**Files:**
- Create: `apps/server/internal/media/orbit_extract.go`
- Create: `apps/server/internal/media/orbit_extract_test.go`

**Interfaces:**
- Consumes: 无
- Produces:

```go
const OrbitFrameCount = 16
const OrbitMinKeepFrames = 8

func OrbitFrameTimes(duration time.Duration, n int) []time.Duration
func OrbitYaw(i, n int) float64

type Frame struct {
    Yaw  float64
    JPEG []byte
}

type Extractor interface {
    Extract(ctx context.Context, video []byte, duration time.Duration, n int) ([]Frame, error)
}

func NewFFMPEGExtractor() Extractor
func FrameTimesOK 的测试锁定 t_i = duration * i / n，最后一帧 i=n-1
```

- [ ] **Step 1: 写失败测试**

`orbit_extract_test.go`：

```go
package media

import (
    "math"
    "testing"
    "time"
)

func TestOrbitFrameTimesSixteen(t *testing.T) {
    duration := 3500 * time.Millisecond
    times := OrbitFrameTimes(duration, 16)
    if len(times) != 16 {
        t.Fatalf("len=%d", len(times))
    }
    if times[0] != 0 {
        t.Fatalf("first=%s", times[0])
    }
    if times[15] != duration*15/16 {
        t.Fatalf("last=%s want %s", times[15], duration*15/16)
    }
    if times[15] >= duration {
        t.Fatal("last frame must not hit EOF")
    }
}

func TestOrbitYawSixteen(t *testing.T) {
    if OrbitYaw(0, 16) != 0 || math.Abs(OrbitYaw(1, 16)-22.5) > 1e-9 || OrbitYaw(8, 16) != 180 {
        t.Fatalf("yaw 0/1/8 = %v %v %v", OrbitYaw(0, 16), OrbitYaw(1, 16), OrbitYaw(8, 16))
    }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/server && go test ./internal/media -run 'TestOrbitFrameTimesSixteen|TestOrbitYawSixteen' -v`

Expected: FAIL，undefined `OrbitFrameTimes` / `OrbitYaw`。

- [ ] **Step 3: 实现公式 + Extractor**

`orbit_extract.go`：

```go
package media

import (
    "bytes"
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "time"
)

const (
    OrbitFrameCount    = 16
    OrbitMinKeepFrames = 8
    orbitJPEGQuality   = "80"
    orbitMaxEdge       = 720
)

func OrbitFrameTimes(duration time.Duration, n int) []time.Duration {
    if n <= 0 {
        return nil
    }
    out := make([]time.Duration, n)
    for i := 0; i < n; i++ {
        out[i] = duration * time.Duration(i) / time.Duration(n)
    }
    return out
}

func OrbitYaw(i, n int) float64 {
    if n <= 0 {
        return 0
    }
    return 360.0 * float64(i) / float64(n)
}

type Frame struct {
    Yaw  float64
    JPEG []byte
}

type Extractor interface {
    Extract(ctx context.Context, video []byte, duration time.Duration, n int) ([]Frame, error)
}

type ffmpegExtractor struct{}

func NewFFMPEGExtractor() Extractor { return ffmpegExtractor{} }

func (ffmpegExtractor) Extract(ctx context.Context, video []byte, duration time.Duration, n int) ([]Frame, error) {
    if _, err := exec.LookPath("ffmpeg"); err != nil {
        return nil, fmt.Errorf("ffmpeg not found: %w", err)
    }
    dir, err := os.MkdirTemp("", "body-orbit-*")
    if err != nil {
        return nil, err
    }
    defer os.RemoveAll(dir)
    in := filepath.Join(dir, "in.mp4")
    if err := os.WriteFile(in, video, 0o600); err != nil {
        return nil, err
    }
    times := OrbitFrameTimes(duration, n)
    frames := make([]Frame, 0, n)
    scale := fmt.Sprintf("scale='if(gt(iw,ih),%d,-2)':'if(gt(ih,iw),%d,-2)'", orbitMaxEdge, orbitMaxEdge)
    for i, ts := range times {
        out := filepath.Join(dir, fmt.Sprintf("%02d.jpg", i))
        sec := fmt.Sprintf("%.3f", ts.Seconds())
        cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-ss", sec, "-i", in,
            "-frames:v", "1", "-vf", scale, "-q:v", "2", out)
        var stderr bytes.Buffer
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
            return frames, fmt.Errorf("ffmpeg frame %d: %w: %s", i, err, stderr.String())
        }
        jpeg, err := os.ReadFile(out)
        if err != nil {
            return frames, err
        }
        frames = append(frames, Frame{Yaw: OrbitYaw(i, n), JPEG: jpeg})
    }
    return frames, nil
}
```

另写一条集成测试 `TestFFMPEGExtractorJPEG`：若 `exec.LookPath("ffmpeg")` 失败则 `t.Skip`；否则对 Task 5 尚未存在的夹具可先用 `ffmpeg -f lavfi` 在测试里现场生成 1 秒色条再抽 16 帧，断言 `len==16` 且每帧 JPEG 以 `0xFF 0xD8` 开头。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/server && go test ./internal/media -v`

Expected: 公式测试 PASS；ffmpeg 存在时集成测试 PASS，否则 Skip。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/media/orbit_extract.go apps/server/internal/media/orbit_extract_test.go
git commit -m "$(cat <<'EOF'
feat: 锁定环绕视频抽帧时间戳并由 ffmpeg 输出 JPEG。

EOF
)"
```

---

### Task 4: 领域类型与迁移

**Files:**
- Modify: `apps/server/internal/domain/domain.go`
- Create: `apps/server/internal/database/migrations/021_body_presentations.sql`

**Interfaces:**
- Consumes: Task 1 / Task 3 的字段名
- Produces: `TaskTypeBodyOrbit`；`BodyOrbitTaskPayload`；`BodyPresentation`；`BodyPresentationInput`；`BodyPresentationStatus`；`OrbitFrame`

- [ ] **Step 1: 在 `domain.go` 的 TaskType 常量区追加**

```go
TaskTypeBodyOrbit TaskType = "body_orbit"
```

在 HairPreviewTaskPayload 旁：

```go
type BodyOrbitTaskPayload struct {
    PresentationID string `json:"presentation_id"`
}
```

在 HairPreview 旁追加（JSON 与 OpenAPI 对齐；`mesh` 用指针以便 v1 输出 `null`）：

```go
type BodyPresentationInput struct {
    BodyMediaID string `json:"body_media_id"`
    FaceMediaID string `json:"face_media_id"`
}

type OrbitFrame struct {
    Yaw float64 `json:"yaw"`
    URL string  `json:"url"`
}

type BodyOrbitView struct {
    VideoURL   string       `json:"video_url,omitempty"`
    DurationMS int          `json:"duration_ms,omitempty"`
    Frames     []OrbitFrame `json:"frames"`
}

type BodyMesh struct {
    Format     string `json:"format"`
    URL        string `json:"url"`
    TextureURL string `json:"texture_url,omitempty"`
}

type BodyPresentation struct {
    ID             string         `json:"id"`
    BodyMediaID    string         `json:"body_media_id"`
    FaceMediaID    string         `json:"face_media_id"`
    Representation string         `json:"representation"`
    Orbit          BodyOrbitView  `json:"orbit"`
    Mesh           *BodyMesh      `json:"mesh"`
    ProviderVersion string        `json:"provider_version,omitempty"`
    Status         string         `json:"status"`
    Progress       int            `json:"progress"`
    Stage          string         `json:"stage"`
    ErrorMessage   string         `json:"error_message,omitempty"`
    Task           *TaskView      `json:"task,omitempty"`
    CreatedAt      time.Time      `json:"created_at"`
    UpdatedAt      time.Time      `json:"updated_at"`
}

type BodyPresentationStatus struct {
    Available bool              `json:"available"`
    Active    *BodyPresentation `json:"active"`
    Completed *BodyPresentation `json:"completed"`
    Failed    *BodyPresentation `json:"failed"`
}
```

`021_body_presentations.sql`：

```sql
CREATE TABLE body_presentations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body_media_id uuid NOT NULL REFERENCES media_assets(id),
  face_media_id uuid NOT NULL REFERENCES media_assets(id),
  representation text NOT NULL DEFAULT 'orbit'
    CHECK (representation IN ('orbit', 'mesh')),
  video_url text NOT NULL DEFAULT '',
  video_storage_key text NOT NULL DEFAULT '',
  duration_ms int NOT NULL DEFAULT 0,
  frames jsonb NOT NULL DEFAULT '[]',
  mesh jsonb,
  provider_version text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX body_presentations_user_created_idx
  ON body_presentations(user_id, created_at DESC);
```

- [ ] **Step 2: 编译确认**

Run: `cd apps/server && go test ./internal/domain -count=0`

Expected: PASS（包能编译即可）。

- [ ] **Step 3: Commit**

```bash
git add apps/server/internal/domain/domain.go \
  apps/server/internal/database/migrations/021_body_presentations.sql
git commit -m "$(cat <<'EOF'
feat: 增加 body_presentations 表和 body_orbit 领域类型。

EOF
)"
```

---

### Task 5: Demo 环绕生成器与夹具

**Files:**
- Create: `apps/server/internal/provider/orbit.go`
- Create: `apps/server/internal/provider/orbit_demo.go`
- Create: `apps/server/internal/provider/orbit_demo_test.go`
- Create: `apps/server/assets/demo/body-orbit.mp4`（用 ffmpeg 生成后入库）
- Modify: `apps/server/internal/provider/ai_runtime.go`（只加常量）

**Interfaces:**
- Consumes: 无厂商名
- Produces:

```go
const CapabilityBodyOrbit = "body_orbit"
const DemoBodyOrbitVersion = "demo-body-orbit-v1"

type OrbitInput struct {
    Body, Face         []byte
    BodyMIME, FaceMIME string
}
type OrbitOutput struct {
    VideoData       []byte
    MIMEType        string
    Duration        time.Duration
    ProviderVersion string
}
type OrbitGenerator interface {
    Generate(context.Context, OrbitInput) (OrbitOutput, error)
}
func NewDemoOrbitGenerator(assetDir string) *DemoOrbitGenerator
```

- [ ] **Step 1: 生成并提交夹具**

在仓库根执行（本机需有 ffmpeg）：

```bash
mkdir -p apps/server/assets/demo
ffmpeg -y -f lavfi -i "testsrc=size=360x640:rate=8:duration=3" \
  -c:v libx264 -pix_fmt yuv420p -an \
  apps/server/assets/demo/body-orbit.mp4
```

文件应小于 400KB。没有 ffmpeg 则不要用 WebM/WebP 凑合，先装 ffmpeg。

- [ ] **Step 2: 写失败测试**

```go
func TestDemoOrbitGeneratorMP4(t *testing.T) {
    gen := NewDemoOrbitGenerator("../assets") // 测试里用 filepath 相对 repo：t.Chdir 到 server 根或传入 cfg.AssetDir 风格
    out, err := gen.Generate(context.Background(), OrbitInput{})
    if err != nil {
        t.Fatal(err)
    }
    if out.MIMEType != "video/mp4" || !bytes.HasPrefix(out.VideoData, []byte{0x00, 0x00, 0x00}) {
        t.Fatalf("not mp4: mime=%s len=%d", out.MIMEType, len(out.VideoData))
    }
    if out.ProviderVersion != DemoBodyOrbitVersion || out.Duration < 2*time.Second || out.Duration > 6*time.Second {
        t.Fatalf("meta %#v", out)
    }
}
```

测试里 `assetDir` 用 `filepath.Join` 找到 `apps/server/assets`（从 `internal/provider` 上两级）。`DemoBodyOrbitVersion` 必须是 `"demo-body-orbit-v1"`。

- [ ] **Step 3: 跑测试确认失败**

Run: `cd apps/server && go test ./internal/provider -run TestDemoOrbitGeneratorMP4 -v`

Expected: FAIL，undefined。

- [ ] **Step 4: 实现接口与 Demo**

`orbit.go` 只放接口、常量和 `OrbitInput` / `OrbitOutput`。

`orbit_demo.go`：`Generate` 读 `{assetDir}/demo/body-orbit.mp4`，`MIMEType=video/mp4`，`Duration=3*time.Second`（与夹具一致；若以后换夹具，用 ffprobe 不是 v1 范围，写死 3s），`ProviderVersion=demo-body-orbit-v1`。文件缺失返回 error。忽略 Body/Face 字节（Demo 不改身份）。

`ai_runtime.go` 的 capability 常量区增加 `CapabilityBodyOrbit = "body_orbit"`。

- [ ] **Step 5: 跑测试确认通过**

Run: `cd apps/server && go test ./internal/provider -run TestDemoOrbitGeneratorMP4 -v`

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add apps/server/internal/provider/orbit.go \
  apps/server/internal/provider/orbit_demo.go \
  apps/server/internal/provider/orbit_demo_test.go \
  apps/server/internal/provider/ai_runtime.go \
  apps/server/assets/demo/body-orbit.mp4
git commit -m "$(cat <<'EOF'
feat: 用 Demo 夹具实现 body_orbit 生成器。

EOF
)"
```

---

### Task 6: 仓储

**Files:**
- Modify: `apps/server/internal/repository/repository.go`
- Create: `apps/server/internal/repository/postgres/body_presentations.go`
- Modify: `apps/server/internal/repository/postgres/postgres.go`（`DeleteUserData` 查询与 `deleteUserDataQueries`、`taskAttemptsCap`）
- Modify: `apps/server/internal/repository/postgres/postgres_test.go`（`TestDeleteUserDataCoversAllUserTables` 表名单）

**Interfaces:**
- Consumes: Task 4 类型
- Produces:

```go
CreateBodyPresentation(ctx, userID, input domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, error)
GetBodyPresentation(ctx, userID, id string) (domain.BodyPresentation, error)
GetBodyOrbitWork(ctx, userID, id string) (domain.BodyPresentationInput, error)
ListBodyPresentationStatus(ctx, userID string) (active, completed, failed *domain.BodyPresentation, err error)
ApplyBodyOrbitResult(ctx, id, videoURL, videoKey string, durationMS int, frames []domain.OrbitFrame, frameKeys []string, providerVersion string) error
```

`CreateBodyPresentation` 若该用户已有 queued/processing 的 `body_orbit`，返回已有行 + 其 task，不插入第二行。

JSONB `frames` 元素：`{"yaw":0,"url":"...","storage_key":"..."}`。读出映射到 `OrbitFrame` 时丢掉 `storage_key`。删除用户时从 `video_storage_key` 与每帧 `storage_key` 收集 key。

- [ ] **Step 1: 在接口上声明上述 5 个方法**（写在发型预览方法块之后）。此时 `go test ./internal/repository/postgres` 会因 `Store` 缺方法失败。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/server && go test ./internal/repository/postgres -count=0`

Expected: FAIL，`Store` does not implement `Repository`。

- [ ] **Step 3: 实现 `body_presentations.go`**

写入逻辑对齐 `CreateHairPreview`：同一事务 `INSERT body_presentations` + `INSERT tasks (type='body_orbit', payload={"presentation_id": id}, status=queued, stage='正在排队')`。去重查询：

```sql
SELECT p.id FROM body_presentations p
JOIN tasks t ON t.type='body_orbit' AND t.payload->>'presentation_id'=p.id::text
WHERE p.user_id=$1 AND t.status IN ('queued','processing')
ORDER BY t.created_at DESC LIMIT 1
```

`ListBodyPresentationStatus`：

- `completed`：`video_url <> ''` 最新一条。
- `active`：最新 queued/processing 任务对应行。
- `failed`：最新 failed 任务对应行，且其 `created_at` 晚于 `completed.created_at`（无 completed 则只要最新失败）。

`ApplyBodyOrbitResult`：按 id 更新 video/frames/provider_version；frames 写入带 `storage_key` 的 jsonb。

`GetBodyOrbitWork`：`SELECT body_media_id, face_media_id WHERE id=$1 AND user_id=$2`，未命中 `ErrNotFound`。

所有读都带 `user_id`。

`postgres.go`：

- `taskAttemptsCap` 保持 `ELSE 2`，`body_orbit` 走 2 次，不必改 CASE。
- `deleteUserDataQueries` 在 `hair_previews` 前加 `` `DELETE FROM body_presentations WHERE user_id=$1` ``。
- `DeleteUserData` 的 key 查询增加：

```sql
UNION ALL SELECT video_storage_key FROM body_presentations WHERE user_id=$1 AND video_storage_key<>''
UNION ALL SELECT key FROM body_presentations p
  CROSS JOIN LATERAL jsonb_array_elements(p.frames) f
  CROSS JOIN LATERAL (SELECT f->>'storage_key' AS key) k
  WHERE p.user_id=$1 AND k.key<>''
```

`postgres_test.go` 的表名单加入 `"body_presentations"`（放在 `hair_previews` 旁）。

- [ ] **Step 4: 跑仓储测试**

Run: `cd apps/server && go test ./internal/repository/postgres -count=1`

Expected: PASS（需本机/CI 已有测试库；与现有 postgres 测试同一前提）。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/repository/repository.go \
  apps/server/internal/repository/postgres/body_presentations.go \
  apps/server/internal/repository/postgres/postgres.go \
  apps/server/internal/repository/postgres/postgres_test.go
git commit -m "$(cat <<'EOF'
feat: 为 body presentations 加上按用户隔离的仓储。

EOF
)"
```

---

### Task 7: Service 创建、status、计费

**Files:**
- Create: `apps/server/internal/service/body_orbit.go`
- Create: `apps/server/internal/service/body_orbit_test.go`
- Modify: `apps/server/internal/service/service.go`（`Service` 字段、`ProviderOptions.Orbit`、`New` 默认、`handlers` 先不注册等 Task 8）
- Modify: `apps/server/internal/service/billing.go`（活跃 look 类型列表）
- Modify: `apps/server/internal/httpapi/httpapi.go` 的 `writeServiceError` 不在本任务改

**Interfaces:**
- Consumes: Task 5 `OrbitGenerator`；Task 6 repo 方法
- Produces:

```go
var ErrCapabilityUnavailable = errors.New("capability unavailable")

func (s *Service) CreateBodyPresentation(ctx, userID string, input domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, error)
func (s *Service) GetBodyPresentation(ctx, userID, id string) (domain.BodyPresentation, error)
func (s *Service) GetBodyPresentationStatus(ctx, userID string) (domain.BodyPresentationStatus, error)
```

规则锁定：

- `s.orbitGenerator == nil` → `fmt.Errorf("%w: 3D 形象暂未开放", ErrCapabilityUnavailable)`，不写库。
- `body` / `face` 必须是合法 UUID；`GetMediaAssetsForUser` 两张都在且 `Kind` 分别为 `body` / `face`，否则 `fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)`。
- 无进行中 `body_orbit` 才 `authorize(..., domainActionLook, "", 1)`；已有进行中则 repo 去重返回，不扣费。
- `CountActiveTasksByTypes` 增加 `string(domain.TaskTypeBodyOrbit)`。
- `attachBodyPresentation`：绝对化 `orbit.video_url` 与每帧 `url`；用 `LatestTasksByRef(..., TaskTypeBodyOrbit, "presentation_id", []string{id})` 投影 status/progress/stage/error/task。
- `GetBodyPresentationStatus`：`Available: s.orbitGenerator != nil`，再填 repo 的 active/completed/failed 并各跑 attach。

- [ ] **Step 1: 写失败测试**

`body_orbit_test.go` 用嵌入 `repository.Repository` 的 stub，只实现本任务用到的方法（`GetMediaAssetsForUser`、`CreateBodyPresentation`、`CountActiveTasksByTypes`、`ApplyBilling` 若走真实 authorize——更简单：对「无 generator」和「kind 错误」两条不碰 billing）。

```go
func TestCreateBodyPresentationRequiresGenerator(t *testing.T) {
    svc := New(/* repo stub */, nil, nil, "http://127.0.0.1", time.Hour, 1, slog.Default())
    _, _, err := svc.CreateBodyPresentation(context.Background(), "user-1", domain.BodyPresentationInput{
        BodyMediaID: "11111111-1111-1111-1111-111111111111",
        FaceMediaID: "22222222-2222-2222-2222-222222222222",
    })
    if !errors.Is(err, ErrCapabilityUnavailable) {
        t.Fatalf("err=%v", err)
    }
}

func TestCreateBodyPresentationRejectsWrongKind(t *testing.T) {
    repo := &bodyOrbitRepoStub{assets: []domain.MediaAsset{
        {ID: "11111111-1111-1111-1111-111111111111", Kind: "outfit"},
        {ID: "22222222-2222-2222-2222-222222222222", Kind: "face"},
    }}
    svc := newBodyOrbitService(repo, &fakeOrbitGen{})
    _, _, err := svc.CreateBodyPresentation(context.Background(), "user-1", domain.BodyPresentationInput{
        BodyMediaID: "11111111-1111-1111-1111-111111111111",
        FaceMediaID: "22222222-2222-2222-2222-222222222222",
    })
    if !errors.Is(err, ErrValidation) {
        t.Fatalf("err=%v", err)
    }
}
```

`fakeOrbitGen` 实现 `Generate` 即可，本任务创建路径不应调用它。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/server && go test ./internal/service -run 'TestCreateBodyPresentation' -v`

Expected: FAIL，方法不存在。

- [ ] **Step 3: 实现 `body_orbit.go` 并改 billing 活跃类型**

`billing.go` 的 `CountActiveTasksByTypes` 调用处：

```go
string(domain.TaskTypeHairPreview), string(domain.TaskTypePlanLook), string(domain.TaskTypeTodayLook), string(domain.TaskTypeBodyOrbit),
```

`Service` 增加 `orbitGenerator provider.OrbitGenerator`；`ProviderOptions` 增加 `Orbit provider.OrbitGenerator`；`New` 把它赋上（默认 nil）。

再补第三条测试：`hasActiveTaskType` 为 true 时 stub 的 `CreateBodyPresentation` 被调用且测试用的 authorize 计数器为 0。可用一个 `authRepo` 记录 `ApplyBilling` 次数；或断言 `Create` 返回的 id 是 stub 里预置的 active id。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/server && go test ./internal/service -run 'TestCreateBodyPresentation|TestAuthorizeIncludesBodyOrbit|TestBilling' -count=1`

Expected: 新测试 PASS；现有 billing 测试仍 PASS。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/service/body_orbit.go \
  apps/server/internal/service/body_orbit_test.go \
  apps/server/internal/service/service.go \
  apps/server/internal/service/billing.go
git commit -m "$(cat <<'EOF'
feat: 创建 3D 形象前校验档案、能力和 look 额度。

EOF
)"
```

---

### Task 8: Worker handler

**Files:**
- Modify: `apps/server/internal/service/tasks.go`
- Modify: `apps/server/internal/service/service.go`（handlers map）
- Create: `apps/server/internal/service/body_orbit_task_test.go`
- Modify: `apps/server/internal/service/media_loader.go` 或复用现有 loader 读两张图

**Interfaces:**
- Consumes: `OrbitGenerator.Generate`；`media.Extractor`；`ApplyBodyOrbitResult`
- Produces: `bodyOrbitTaskHandler`；`processBodyOrbit`；`taskMaxAttempts[TaskTypeBodyOrbit]=2`；`taskTimeouts[TaskTypeBodyOrbit]=5*time.Minute`；`taskTypeOrder` 追加 `TaskTypeBodyOrbit`

进度：12% `正在读取全身和正脸`；40% `正在生成环绕预览`；72% `正在抽出转盘静帧`；95% `正在保存`。

失败规则：

- 解码 payload / 行不存在 → `ErrTaskRemoved` 或永久失败。
- `Generate` 返回非 `video/mp4` 或空字节 → `newPermanentTaskError`。
- `Generate` 其它 error → 可重试。
- 抽帧 error 或 `len(frames) < 8`：仍保存视频，`frames=[]`，任务 completed，打 WARN。
- 存对象：`{userID}/generated/body-orbit/{presentationID}.mp4` 与 `{userID}/generated/body-orbit/{presentationID}/{i}.jpg`。
- `constrainEditImage` 后把 body/face 交给 Generator（与 look 编辑同一 `editBudget`）。

`Service` 增加 `orbitExtractor media.Extractor`；`New` 默认 `media.NewFFMPEGExtractor()`；测试注入假抽取器。

- [ ] **Step 1: 写失败测试**

假 generator 返回 3 字节 `'f','t','y','p'` 不够，用 Task 5 夹具字节。假 extractor：

```go
type stubExtract struct{ frames []media.Frame; err error }
```

用例 A：Generate 出夹具 MP4 + extract 16 帧 → `ApplyBodyOrbitResult` 收到 16 帧、videoURL 非空。  
用例 B：extract 返回 3 帧 → Apply 的 frames 长度为 0，仍有 videoURL。  
用例 C：Generate MIME `video/webm` → 永久失败，不 Apply。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/server && go test ./internal/service -run 'TestProcessBodyOrbit' -v`

Expected: FAIL。

- [ ] **Step 3: 实现 `processBodyOrbit` 并注册 handler**

在 `tasks.go` 增加 `bodyOrbitTaskHandler` 与 `Handle` → `processBodyOrbit`。读媒体用现有 `NewAnalysisMediaLoader` 同一套 `GetMediaAssets` + storage（handler 里按 `GetBodyOrbitWork` 的两个 id 加载）。把结果 URL 写成 `"/uploads/"+key`，与 hair 一致。

`service.go` 的 `handlers` map 增加 `domain.TaskTypeBodyOrbit: bodyOrbitTaskHandler{service}`。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/server && go test ./internal/service -run 'TestProcessBodyOrbit|TestFailWrite' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/service/tasks.go \
  apps/server/internal/service/service.go \
  apps/server/internal/service/body_orbit.go \
  apps/server/internal/service/body_orbit_task_test.go
git commit -m "$(cat <<'EOF'
feat: 注册 body_orbit 任务：出视频、抽帧、残帧降级。

EOF
)"
```

---

### Task 9: HTTP、启动接线、本地路由

**Files:**
- Create: `apps/server/internal/httpapi/body.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`（路由 + `writeServiceError`）
- Modify: `apps/server/internal/bootstrap/ai.go`
- Modify: `apps/server/cmd/api/main.go`
- Modify: `apps/server/config/ai-routing.example.json`
- Modify: `apps/server/internal/config/ai_routing.go`（协议白名单 + capability 白名单）
- Modify: `apps/server/internal/config/config_test.go`（若 `TestExampleAIRoutingConfigIsValid` 因新键失败则一起修）

**Interfaces:**
- Consumes: Task 7 service 方法；`provider.CapabilityBodyOrbit`
- Produces: `POST /v1/body-presentations` → 202；`GET /v1/body-presentations/status`；`GET /v1/body-presentations/{id}`；503 `capability_unavailable`

- [ ] **Step 1: `writeServiceError` 增加分支**

```go
case errors.Is(err, service.ErrCapabilityUnavailable):
    writeError(w, r, http.StatusServiceUnavailable, "capability_unavailable",
        strings.TrimPrefix(err.Error(), service.ErrCapabilityUnavailable.Error()+": "))
```

`body.go`：

```go
func (a *API) createBodyPresentation(w http.ResponseWriter, r *http.Request) {
    var input domain.BodyPresentationInput
    if err := decodeJSON(r, &input); err != nil {
        writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
        return
    }
    item, task, err := a.service.CreateBodyPresentation(r.Context(), currentUser(r).ID, input)
    if err != nil {
        a.writeServiceError(w, r, err)
        return
    }
    writeDataTask(w, http.StatusAccepted, item, domainTaskRef(task))
}

func (a *API) getBodyPresentationStatus(w http.ResponseWriter, r *http.Request) {
    item, err := a.service.GetBodyPresentationStatus(r.Context(), currentUser(r).ID)
    if err != nil {
        a.writeServiceError(w, r, err)
        return
    }
    writeData(w, http.StatusOK, item)
}

func (a *API) getBodyPresentation(w http.ResponseWriter, r *http.Request) {
    item, err := a.service.GetBodyPresentation(r.Context(), currentUser(r).ID, r.PathValue("id"))
    if err != nil {
        a.writeServiceError(w, r, err)
        return
    }
    writeData(w, http.StatusOK, item)
}
```

在 `httpapi.go` 注册（`/status` 必须注册在 `/{id}` 之前）：

```go
mux.Handle("POST /v1/body-presentations", api.auth(http.HandlerFunc(api.createBodyPresentation)))
mux.Handle("GET /v1/body-presentations/status", api.auth(http.HandlerFunc(api.getBodyPresentationStatus)))
mux.Handle("GET /v1/body-presentations/{id}", api.auth(http.HandlerFunc(api.getBodyPresentation)))
```

- [ ] **Step 2: Bootstrap 接线**

`AIBundle` 加 `Orbit provider.OrbitGenerator`。`BuildAI` 末尾：

```go
if runtime.HasRoute(provider.CapabilityBodyOrbit) {
    bundle.Orbit = provider.NewDemoOrbitGenerator(cfg.AssetDir)
}
```

v1 只要路由存在就接 Demo。`ai-routing.production.json` **不要**加 `body_orbit`。

`validateAIRouting` 目前会拒绝未知 capability 和未知 protocol。必须同时改三处，否则 example.json 无法加载：

```go
protocols := map[string]bool{
    "openai_responses":         true,
    "openai_chat_completions":  true,
    "openai_image_edit":        true,
    "dashscope_wan":            true,
    "dashscope_wanx_imageedit": true,
    "ark_image":                true,
    "demo_orbit":               true,
}
videoCapabilities := map[string]bool{"body_orbit": true}
```

capability 循环改成：

```go
if !structuredCapabilities[capability] && !imageCapabilities[capability] && !videoCapabilities[capability] {
    return fmt.Errorf("AI route %q is not a supported capability", capability)
}
```

协议配对在 image/structured 两条之后加：

```go
if videoCapabilities[capability] && protocol != "demo_orbit" {
    return fmt.Errorf("AI route %q uses unsupported video protocol %q", capability, protocol)
}
```

v1 只允许 `demo_orbit`。以后接真视频协议时再把新 protocol 加进 `protocols` 与这条白名单，不改业务包。

`ai-routing.example.json` 增加（**不要**写入 `ai-routing.production.json`）：

```json
"demo/body-orbit": {
  "vendor": "demo",
  "protocol": "demo_orbit",
  "model": "body-orbit",
  "base_url": "http://127.0.0.1/demo-orbit",
  "api_key_env": "AI_TEST_PRIMARY_KEY",
  "timeout_seconds": 30
}
```

```json
"body_orbit": {
  "policy": "quality_first",
  "primary": "demo/body-orbit"
}
```

`api_key_env` 只为过 `validateAIRouting` 的非空检查；Demo 生成器不读这个 key。非生产启动不校验该 env 是否存在。

`main.go` 的 `ProviderOptions` 加 `Orbit: ai.Orbit`。

`NewAIRuntime` 不会按 protocol 发视频请求：`body_orbit` 在 bootstrap 里直接接到 `DemoOrbitGenerator`，runtime 只提供 `HasRoute`。

- [ ] **Step 3: 编译与单测**

Run: `cd apps/server && go test ./internal/httpapi ./internal/bootstrap ./internal/config -count=1`

Expected: PASS。生产配置测试不得要求 `body_orbit`。

- [ ] **Step 4: Commit**

```bash
git add apps/server/internal/httpapi/body.go \
  apps/server/internal/httpapi/httpapi.go \
  apps/server/internal/bootstrap/ai.go \
  apps/server/cmd/api/main.go \
  apps/server/config/ai-routing.example.json \
  apps/server/internal/config/ai_routing.go \
  apps/server/internal/config/config_test.go \
  apps/server/internal/provider/ai_runtime.go
git commit -m "$(cat <<'EOF'
feat: 开放 body-presentations HTTP，并在示例路由接上 Demo。

EOF
)"
```

---

### Task 10: ffmpeg 生产门禁与镜像

**Files:**
- Modify: `apps/server/Dockerfile`
- Modify: `apps/server/cmd/api/main.go`
- Modify: `apps/server/internal/config/config.go` 或只在 `main` 检查（优先 `main`，避免 config 包引入 `os/exec`）

**Interfaces:**
- Consumes: `media.NewFFMPEGExtractor` 已用 PATH 上的 `ffmpeg`
- Produces: 生产缺 ffmpeg 则进程退出；镜像含 ffmpeg

- [ ] **Step 1: Dockerfile 运行镜像安装 ffmpeg**

把

```
RUN apk add --no-cache ca-certificates \
```

改成：

```
RUN apk add --no-cache ca-certificates ffmpeg \
```

- [ ] **Step 2: 生产启动检查**

在 `main.go` 加载 config 成功之后：

```go
if cfg.Environment == "production" {
    if _, err := exec.LookPath("ffmpeg"); err != nil {
        logger.Error("ffmpeg is required in production", "error", err)
        os.Exit(1)
    }
}
```

`Environment` 字段名以 `config.Config` 现有为准（与 `APP_ENV=production` 那扇门同一字段）。开发缺 ffmpeg 不退出。

- [ ] **Step 3: 编译**

Run: `cd apps/server && go vet ./... && go test ./internal/config -count=1`

Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add apps/server/Dockerfile apps/server/cmd/api/main.go
git commit -m "$(cat <<'EOF'
chore: 生产 Worker 镜像带上 ffmpeg，缺了就拒绝启动。

EOF
)"
```

---

### Task 11: 小程序 `BodyViewer`

**Files:**
- Create: `apps/miniapp/src/components/body-viewer/index.config.ts`
- Create: `apps/miniapp/src/components/body-viewer/index.tsx`
- Create: `apps/miniapp/src/components/body-viewer/index.scss`

**Interfaces:**
- Consumes: `lookVideo`、`lookImage`、`userImage`、`LAB_COPY`、`CompareSlider`、`ExampleImage` 的 badge 由调用方传入
- Produces: `<BodyViewer presentation bodyImageURL badgeText />`

- [ ] **Step 1: 配置隔离**

`index.config.ts`：

```ts
export default {
  styleIsolation: 'apply-shared',
  virtualHost: true,
}
```

- [ ] **Step 2: 实现组件**

Props：

```ts
interface BodyViewerProps {
  presentation: BodyPresentation
  bodyImageURL: string
  badgeText: string
}
```

逻辑：

1. `video = lookVideo(presentation.orbit.video_url)`。
2. `frames = presentation.orbit.frames.map(f => ({ ...f, url: lookImage(f.url) })).filter(f => f.url)`。
3. `useState`：`mode: 'video' | 'turntable'`。有 video 时初始 `'video'`，否则若 `frames.length>=1` 为 `'turntable'`。
4. `<Video src={video} muted autoplay controls={false} showCenterPlayBtn={false} objectFit="contain" onEnded={() => setMode('turntable')} />` 仅当 `mode==='video' && video`。
5. 转盘：`index` state，默认 0。`onTouchStart/Move` 用 `deltaX`，每移动 `36rpx` 等价像素用 `Taro.pxTransform` 或按 `clientX` 每 18px 一帧（小程序里用 `e.changedTouches[0].clientX`，阈值 18）。`index = (index + dir + frames.length) % frames.length`。松手不惯性多跳。`key={\`${frame.yaw}-${frame.url}\`}`。
6. 中途在视频上 `onTouchStart` → `setMode('turntable')`。
7. 对比：仅 `frames.length >= 8`。`ExampleImage` 现有 props 是 `src` / `user` / `badgeText` / `anchor`，不要加新 prop：

```tsx
<CompareSlider
  current={<ExampleImage user src={userImage(bodyImageURL)} anchor="top" />}
  plan={<ExampleImage src={frames[0].url} badgeText={badgeText} anchor="top" />}
/>
```
8. `frames.length < 8` 且非 video-only 转盘不足：不渲染滑杆；若只有视频，显示 `<Text>{LAB_COPY.noCompare}</Text>`。
9. 禁 `{frames.length && ...}`，改 `frames.length > 0 ? ... : null`。
10. 样式只写 rpx；容器高度约 `880rpx`；图 `width: 100%`；`object-fit` 在 scss 用 `mode` 不写裸 px。

- [ ] **Step 3: 静态门禁**

Run: `node apps/miniapp/scripts/check.mjs`

Expected: PASS，无裸 `px`、无 WebP。

- [ ] **Step 4: Commit**

```bash
git add apps/miniapp/src/components/body-viewer
git commit -m "$(cat <<'EOF'
feat: 增加先播环绕再拖转盘的 BodyViewer。

EOF
)"
```

---

### Task 12: 实验室页、任务中心、全量门禁

**Files:**
- Modify: `apps/miniapp/src/packages/tools/pages/lab/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/lab/index.scss`
- Modify: `apps/miniapp/src/services/storage.ts`
- Modify: `apps/miniapp/src/services/task-utils.ts`
- Modify: `apps/miniapp/src/pages/profile/index.tsx` 仅当「我的」任务中心跳转需要（`openTask` 已覆盖）

**Interfaces:**
- Consumes: `api.getBodyPresentationStatus`、`api.createBodyPresentation`、`api.getCurrentAnalysis`、`useTaskPolling`、`LAB_COPY`、`IMAGE_BADGE_COPY`、`BodyViewer`、`ErrorState`、`EmptyState`
- Produces: 实验室 3D 卡按 status 状态机工作；`STORAGE_KEYS.activeTaskBodyOrbit = 'zsm_active_task_body_orbit'`

- [ ] **Step 1: storage + task-utils**

`storage.ts` 增加 `activeTaskBodyOrbit: 'zsm_active_task_body_orbit'`。

`task-utils.ts`：

```ts
    case 'body_orbit':
      return '正在生成 3D 形象'
```

`taskDoneTitle`：

```ts
    case 'body_orbit':
      return '3D 形象已生成'
```

`openTask`：

```ts
    case 'body_orbit':
      Taro.navigateTo({ url: '/packages/tools/pages/lab/index' })
      break
```

- [ ] **Step 2: 改实验室 3D 卡**

进入页：`Promise.all([api.getBodyPresentationStatus(), api.getCurrentAnalysis()])`。

`face = analysis?.media?.find(m => m.kind === 'face')`  
`body = analysis?.media?.find(m => m.kind === 'body')`

局部 state：`status: BodyPresentationStatus | null`；`viewing: 'auto' | 'completed' | 'failed'`（`viewing==='completed'` 表示用户点了「看上一圈」）。

3D 卡渲染（发型 AR / 试衣两卡保持原样）：

1. `!status?.available`：候补，沿用现有 `act` 候补名单。
2. `available && (!face || !body)`：`EmptyState` 文案 `LAB_COPY.empty`，按钮去 `/pages/capture/index`。
3. `status.active`：进度条 / `stage` / `progress`。`useTaskPolling({ id: status.active.task.id, interval: POLL_INTERVALS.bodyOrbit, onUpdate, onFailed })`。`onUpdate` 在 completed 时 `getBodyPresentationStatus` 刷新；failed 切错误态。
4. `!active && status.failed && viewing !== 'completed'`：`ErrorState` 文案 `LAB_COPY.failed`，主按钮 `LAB_COPY.retry` → POST；若 `status.completed` 另给 `LAB_COPY.viewLast` → `setViewing('completed')`。错误与 BodyViewer **不同屏**。
5. `status.completed && ( !status.failed || viewing==='completed' || !需要展示错误 )`：按第 4 条优先失败。无失败或已看上一圈：渲染 `BodyViewer`，`badgeText` = `completed.provider_version` 以 `demo` 开头或 `body.demo` → `IMAGE_BADGE_COPY.demo`，否则 `LAB_COPY.badgeAI`。`bodyImageURL = userImage(body.url)`。按钮 `LAB_COPY.regenerate`。
6. 否则主按钮 `LAB_COPY.generate`。

POST：

```ts
const { data } = await api.createBodyPresentation({
  body_media_id: body.id,
  face_media_id: face.id,
})
writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, data.id)
```

`handleBillingError` 与发型页相同。`created.task` 驱动轮询。

恢复：读 `activeTaskBodyOrbit`，有则 `getBodyPresentation`；无则靠 status.active。

`usePageShell` / `useDidShow` 与现页一致，防重复加载。

- [ ] **Step 3: 样式**

3D 卡展开 BodyViewer 时全宽，卡片内边距保持 rpx。不要引入 px。

- [ ] **Step 4: 门禁**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
node contracts/scripts/check-sync.mjs
make server-vet && make server-test
```

Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add apps/miniapp/src/packages/tools/pages/lab \
  apps/miniapp/src/services/storage.ts \
  apps/miniapp/src/services/task-utils.ts
git commit -m "$(cat <<'EOF'
feat: 实验室 3D 形象 Lite 接上生成、轮询和对比。

EOF
)"
```

---

## 手动真机（实现完成后，不单列任务）

1. 本地 `ai-routing.example.json` 含 `body_orbit`：实验室 3D 卡可生成；Demo 角标「效果示例」。
2. 播完自动进转盘；拖一周首尾相接；≥8 帧出现对比滑杆。
3. 切后台停止轮询。
4. 去掉路由键：卡回候补，不出现空播放器。
5. 无正脸/全身：空态去拍摄。
6. 再生成失败：「看上一圈」仍显示上一成功结果。
7. `APP_ENV=production` 且 PATH 无 ffmpeg：进程退出。

---

## Spec coverage

| 规格 | 任务 |
|---|---|
| lookVideo / 禁 WebP WebM | 1 |
| LAB_COPY | 1 |
| OpenAPI + check-sync + TaskType | 1–2 |
| yaw 公式、16 抽、&lt;8 丢弃 | 3、8 |
| 表 021、领域、mesh null | 4 |
| Demo 夹具 + capability 名 | 5、9 |
| 仓储隔离、删除 key | 6 |
| 校验 kind、去重不扣费、look 计费 | 7 |
| Worker 步骤与降级 | 8 |
| HTTP 三路由、503、example 路由 | 9 |
| ffmpeg 生产门禁 + 镜像 | 10 |
| BodyViewer 交互 | 11 |
| 实验室状态机、轮询、任务中心 | 12 |
| 不做 C / 不上首页 / 不改原型 | 全局约束 |
| 生产不强制 body_orbit | 9、10 |
