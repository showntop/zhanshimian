# Assessment / Report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立从三槽位 PhotoSet、照片技术/内容/同人检查到经证据核验后发布不可变 Report/Finding 的完整链路，并让小程序报告页只消费新报告契约。

**Architecture:** 服务端保持模块化单体和 `httpapi → service/assessment → repository/provider/storage` 依赖方向；客户端只观察 Operation，内部只注册 `assessment` Task。Handler 的 `Execute` 在事务外完成照片门禁、报告生成和证据核验，并幂等写入仅 Gate 通过的不可变报告图；`Commit` 再以 Task lease CAS 把报告关联到 AnalysisRun、推进当前报告指针并结束 Task/Operation，未 Commit 的报告不可被读取。

**Tech Stack:** Go 1.24、PostgreSQL 16、pgx/v5、Go `image` 标准库、OpenAPI 3.1、TypeScript 5.6/5.9、Taro 4.2、React 18、Node 22、pnpm 10。

## Global Constraints

- 只实现 PhotoSet、三槽位校验、照片技术/内容/同人检查、AnalysisRun、不可变 Report/Finding、报告证据 Gate、Assessment/Report API、服务端测试、金集 fixture 与报告页接线；不实现 PlanSet、RenderSpec、RenderRun、Candidate、Publication 或任何方案/图片渲染逻辑。
- `appearance-coach-prototype/` 只读；根级 `package.json`、`tsconfig.base.json`、`Makefile`、`docker-compose.yml` 不修改。
- 包名固定为 `@zsm/core`、`@zsm/design`、`@zsm/miniapp`、`@zsm/mobile`；Go module 固定为 `github.com/zhanshimian/server`。
- 服务端依赖方向固定为 `httpapi → service → repository / provider / storage`；handler 不直接访问数据库。
- 使用基础计划交付的 `MediaAsset`、`Operation`、`Task`、`ClaimTask(SKIP LOCKED)`、lease/heartbeat/CAS、Provider Invocation 账本和 Task Handler Registry；本计划只注册 `assessment`，不修改任务主循环。
- 客户端只轮询 Operation，不读取 Task；进度由服务端固定 stage 映射，客户端不自行推算业务进度。
- 所有用户资源包含 `id uuid primary key`、`user_id uuid not null`、`created_at timestamptz not null`、`unique(user_id, id)`；跨资源关系使用 `(user_id, parent_id)` 复合外键，越权统一 404。
- 数据库只保存 COS object key，不保存签名 URL；读取报告时由 Media Reader 生成短期签名 URL。
- 上传只接受 JPEG/PNG；Assessment 再次核对文件魔数、MIME、解码结果、尺寸和像素上限；WebP 一律拒绝。
- `photo_sets.content_hash` 只由 `schema_version + face.sha256 + side.sha256 + body.sha256` 的规范化序列计算；相同用户、相同 hash 复用 PhotoSet。
- `analysis_runs.input_hash` 由 `photo_set.content_hash + profile_snapshot + analyzer_schema_version + quality_policy_version` 计算；相同用户、相同 hash 复用已存在 AnalysisRun/Operation，不重复创建 AI 调用。
- Report、ReportFinding、QualityEvaluation 都是 append-only；重新分析创建新 ID，不更新或删除旧产物。
- Finding 数量为 3–6；类别只允许 `hair | makeup | outfit | color`；`priority` 只允许 `1 | 2 | 3`，表示建议先后而非人的严重程度。
- `visible_observation` 只描述照片中可见事实；`recommendation` 与 observation 分开存储；每条 Finding 必须关联一张 PhotoSetItem 和一个归一化矩形区域。
- 不持久化人脸 embedding；同人比对只在内存中执行。
- 不打颜值分、身材分、年龄分，不保存或返回 `beauty_score`、`body_score`、排名或百分位；不做医学、健康、族裔、性格或社会身份推断。
- `confidence` 只存在于 Provider DTO、Service Gate 和 `quality_evaluations.internal_scores`，不得出现在 HTTP DTO、OpenAPI schema、`@zsm/core` 公共类型或小程序 UI。
- 身高、职业、预算、偏好只进入建议上下文，不能被写成视觉观察。
- 内容或同人检查不能确认时失败关闭；同人失败统一用户文案为“照片差异较大，请重新拍摄确认”，不对用户断言“不是本人”。
- 所有用户可见错误态与内容不同屏，并提供“重新拍摄”“重试”或“返回”动作；错误响应固定为 `{"error":{"code","message","request_id","retryable"}}`。
- API 成功包固定为 `{"data": ...}`；异步创建固定为 `202 {"data": <resource>, "operation": <operation-ref>}`；所有创建请求要求 `Idempotency-Key`。
- API 不返回 Provider 厂商、模型名、临时 Provider URL、内部质量分数或 `provider_invocation_id`。
- Demo 只可由服务端 `/v1/media/demo` 和 Demo Provider 产生；用户资产与 Demo 资产不得混合组成 PhotoSet；`provider_version` 以 `demo` 开头的结果在报告 DTO 中固定为 `source_kind: demo_example`、`display_label: 效果示例`。
- 小程序不根据 URL、扩展名或 Provider 名推断来源；用户图无效时保持可见空态，不回退内置图。
- 不增加 `/v2`，不保留旧 `analyses.media_ids[]`、旧 `/v1/analyses*`、旧 Task DTO、旧 `analysis_id/current_image_url/severity/detail/photo` 报告字段或本地 reportId/taskId 兼容分支。
- AI 只经能力路由；业务代码不得出现厂商或模型名。本计划使用 `photo_quality_check`、`photo_identity_consistency`、`appearance_analysis`、`report_evidence_verification` 四个 capability。
- 每次 Provider 调用均写 Provider Invocation 账本；不得记录图片字节、data URL、完整照片 URL、人脸 embedding、手机号或 OpenID。
- 报告成功指标：照片角色/归属准确率 100%，Finding 证据关联完整率 100%，锁定集证据支持率不低于 95%，敏感推断/颜值评分/身材评分为 0，报告 P95 不高于 90 秒。
- 提交边界是计划执行时的原子审查点；本次编写计划不执行任何 `git add`、`git commit` 或业务代码变更。

## Cross-plan Ownership Override

- 本计划拥有 Assessment/Report 的 Go Domain、Service、Postgres、Provider、HTTP 和 `apps/server/eval/assessment/`；它消费 Rule/Contract Freeze 已确定的 OpenAPI，不修改 `contracts/**`。
- Foundation 独占并一次性创建最终 `001_baseline.sql`；本计划只验证 Assessment/Report 表、约束与触发器，不修改 baseline。正文中的 DDL 是 Foundation Task 1 必须纳入的规范和本计划 schema test 的期望。
- 本计划不得修改或提交 `packages/core/**`、`apps/miniapp/**`；下文 Task 10 与 Task 11 的客户端类型、端点、文案和页面接线全部移交 `2026-09-12-miniapp-quality-loop.md`。
- 本计划不修改中央 `bootstrap/api.go`、`bootstrap/worker.go` 或 `httpapi/httpapi.go` composition；Task 8 只提供 Assessment Service/Handler 构造函数和独立 wiring test，最终注册由 Peripherals/Cutover 完成。
- Task 9 只完成服务端 Handler 与 HTTP 契约测试；OpenAPI schema 已在 Rule/Contract Freeze 中完成，它不要求旧手写 Core Client 同步。
- 证据置信阈值不得硬编码为 `0.95`。`quality_policy_version` 必须引用由锁定金集校准的策略：选择能让证据 precision ≥95% 且 recall ≥80% 的最低阈值；运行时代码只读取策略值。
- 本计划只创建 `apps/server/eval/assessment/`；统一 `apps/server/cmd/eval/main.go` 由最终 Peripherals/Cutover 一次装配。不得创建 `cmd/assessment-eval`。
- Assessment Handler 遵循总索引的 disposition 语义：`Execute` 只执行外部调用并写不可读 staged Report/QualityEvaluation；照片拒绝、内容补生成和领域失败都作为 `TaskResult` 返回，由 `Commit` 在 lease CAS 事务中失败 Operation、创建后继 Task 或发布 Report。`Execute` 不得调用 `h.fail`/`h.reject` 结束 Task。
- 本计划不删除旧 `httpapi/analyses.go`、旧 Service 或旧 Miniapp 路径；物理删除统一由 Peripherals/Cutover 与 Miniapp 删除任务完成。正文 Delete 步骤作为最终清理清单跳过。

## 基础计划前置接口

执行本计划前，先确认基础计划已经提供以下名字和语义；若文件尚未落盘，基础计划必须先按这些签名落盘，本计划不得创建第二套同义类型：

```go
// apps/server/internal/domain/media.go
type MediaAsset struct {
	ID, UserID, ObjectKey, SHA256, MIMEType string
	Origin      MediaOrigin
	Purpose     MediaPurpose
	State       MediaState
	ByteSize    int64
	Width, Height int
}

// apps/server/internal/domain/operation.go
type Operation struct {
	ID, UserID, SubjectType, SubjectID string
	Kind OperationKind
	Status OperationStatus
	ProgressBPS int
	StageCode, PublicMessage, ErrorCode string
	Retryable bool
	ResultType, ResultID string
}

// apps/server/internal/domain/task.go
type Task struct {
	ID, UserID, OperationID, Type, SubjectType, SubjectID string
	SubjectGeneration, Attempt, MaxAttempts int
	LeaseToken string
	PayloadVersion int
	Payload json.RawMessage
}
```

```go
// apps/server/internal/service/taskrunner/ports.go
type Handler interface {
	Type() domain.TaskType
	Execute(context.Context, domain.TaskLease) (domain.TaskResult, error)
	Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
}

// apps/server/internal/domain/task.go
type TaskResult struct {
	ResultType string
	ResultID   string
}
```

```go
// apps/server/internal/provider/ai/runtime.go
type StructuredRequest struct {
	Capability, SchemaName string
	Instructions, Prompt   string
	Images                 []ImageInput
	Schema                 map[string]any
	MaxOutputTokens        int
	Validate               func([]byte) error
}

type ImageInput struct {
	AssetID string
	Role    string
	MIMEType string
	Data    []byte
}

type StructuredResult struct {
	JSON []byte
	Meta InvocationMeta
}

type InvocationMeta struct {
	InvocationID string
	Capability string
}

type Runtime interface {
	Structured(context.Context, StructuredRequest) (StructuredResult, error)
}
```

Operation 固定阶段如下，数据库保存 basis points，HTTP 返回整数 basis points，不转换成百分比：

```go
const (
	StageAssessmentQueued      = "assessment.queued"       // 0
	StagePhotoTechnicalCheck   = "photo.technical_check"   // 1000
	StagePhotoContentCheck     = "photo.content_check"     // 2500
	StagePhotoIdentityCheck    = "photo.identity_check"    // 4000
	StageReportGenerating      = "report.generating"       // 5500
	StageEvidenceVerifying     = "report.evidence_check"   // 8000
	StageReportPublishing      = "report.publishing"       // 9500
	StageAssessmentSucceeded   = "assessment.succeeded"    // 10000
)
```

---

### Task 1: Assessment 领域模型与纯校验

**Files:**
- Modify: `apps/server/internal/domain/assessment.go`
- Create: `apps/server/internal/service/assessment/validation.go`
- Test: `apps/server/internal/service/assessment/validation_test.go`

**Interfaces:**
- Consumes: 基础计划的 `domain.MediaAsset`。
- Produces: `domain.PhotoSlots`、`domain.PhotoSet`、`domain.PhotoSetItem`、`domain.AnalysisRun`、`domain.Report`、`domain.ReportFinding`、`ValidateSlots`、`PhotoSetContentHash`、`AnalysisInputHash`、`ReportContentHash`、`ValidateReportDraft`。

- [ ] **Step 1: 写失败的领域校验测试**

```go
package assessment

func TestValidateSlotsRequiresThreeDistinctOwnedReadyAssets(t *testing.T) {
	userID := uuid.NewString()
	assets := validAssets(userID)
	tests := []struct {
		name string
		slots domain.PhotoSlots
		mutate func(map[string]domain.MediaAsset)
		code string
	}{
		{"missing side", domain.PhotoSlots{FaceAssetID: "face", BodyAssetID: "body"}, nil, "photo_slots_invalid"},
		{"duplicate asset", domain.PhotoSlots{FaceAssetID: "face", SideAssetID: "face", BodyAssetID: "body"}, nil, "photo_slots_invalid"},
		{"foreign owner", validSlots(), func(v map[string]domain.MediaAsset) { a := v["side"]; a.UserID = uuid.NewString(); v["side"] = a }, "photo_asset_not_found"},
		{"wrong purpose", validSlots(), func(v map[string]domain.MediaAsset) { a := v["body"]; a.Purpose = domain.MediaPurposeFace; v["body"] = a }, "photo_role_mismatch"},
		{"not ready", validSlots(), func(v map[string]domain.MediaAsset) { a := v["face"]; a.State = domain.MediaStateQuarantined; v["face"] = a }, "photo_asset_not_ready"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copy := maps.Clone(assets)
			if tc.mutate != nil { tc.mutate(copy) }
			err := ValidateSlots(userID, tc.slots, copy)
			var rejected *ValidationError
			if !errors.As(err, &rejected) || rejected.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}

func TestPhotoSetContentHashIsRoleOrdered(t *testing.T) {
	first := PhotoSetContentHash("photo-set.v1", map[domain.PhotoRole]string{
		domain.PhotoRoleFace: "face-sha", domain.PhotoRoleSide: "side-sha", domain.PhotoRoleBody: "body-sha",
	})
	second := PhotoSetContentHash("photo-set.v1", map[domain.PhotoRole]string{
		domain.PhotoRoleBody: "body-sha", domain.PhotoRoleFace: "face-sha", domain.PhotoRoleSide: "side-sha",
	})
	if first != second { t.Fatalf("map order changed hash") }
}

func TestValidateReportDraftRejectsScoresAndIncompleteEvidence(t *testing.T) {
	draft := validDraft()
	draft.Findings[0].VisibleObservation = "颜值 90 分"
	if err := ValidateReportDraft(draft); !errors.Is(err, ErrCopyPolicy) {
		t.Fatalf("got %v", err)
	}
	draft = validDraft()
	draft.Findings[0].Anchor.W = 0
	if err := ValidateReportDraft(draft); !errors.Is(err, ErrEvidenceMissing) {
		t.Fatalf("got %v", err)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/service/assessment -run 'TestValidateSlots|TestPhotoSetContentHash|TestValidateReportDraft' -v`

Expected: FAIL，编译器报告 `PhotoSlots`、`ValidateSlots`、`ValidateReportDraft` 未定义。

- [ ] **Step 3: 实现领域类型和纯校验**

```go
// domain/assessment.go
type PhotoRole string
const (
	PhotoRoleFace PhotoRole = "face"
	PhotoRoleSide PhotoRole = "side"
	PhotoRoleBody PhotoRole = "body"
)

type PhotoSlots struct {
	FaceAssetID string `json:"face_asset_id"`
	SideAssetID string `json:"side_asset_id"`
	BodyAssetID string `json:"body_asset_id"`
}

type PhotoSet struct {
	ID, UserID, ContentHash, SchemaVersion string
	ProfileSnapshot json.RawMessage
	Items []PhotoSetItem
	CreatedAt time.Time
}

type PhotoSetItem struct {
	ID, UserID, PhotoSetID, MediaAssetID string
	Role PhotoRole
	Asset MediaAsset
}

type AnalysisOutcome string
const (
	AnalysisOutcomePublished AnalysisOutcome = "published"
	AnalysisOutcomeRejected AnalysisOutcome = "rejected"
	AnalysisOutcomeFailed AnalysisOutcome = "failed"
)

type AnalysisRun struct {
	ID, UserID, PhotoSetID, OperationID, InputHash string
	AnalyzerSchemaVersion, QualityPolicyVersion string
	ProfileSnapshot json.RawMessage
	ProviderInvocationID, ReportID string
	Outcome *AnalysisOutcome
	CreatedAt time.Time
	FinishedAt *time.Time
}

type EvidenceAnchor struct { X, Y, W, H float64 }

type ReportFinding struct {
	ID, UserID, ReportID, Label, VisibleObservation, Recommendation string
	SourcePhotoItemID string
	Category string
	Priority int
	Anchor EvidenceAnchor
	Confidence float64
	Position int
}

type Report struct {
	ID, UserID, PhotoSetID, HeroAssetID, SchemaVersion, ContentHash string
	PriorityTitle, PriorityCopy string
	ImpressionTags []string
	ProfileSnapshot json.RawMessage
	ProviderInvocationID, QualityEvaluationID string
	Findings []ReportFinding
	CreatedAt time.Time
}

type DraftFinding struct {
	Key, Category, Label, VisibleObservation, Recommendation string
	Priority, Position int
	SourceRole PhotoRole
	Anchor EvidenceAnchor
	Confidence float64
}

type ReportDraft struct {
	ImpressionTags []string
	PriorityTitle, PriorityCopy string
	Findings []DraftFinding
}

type ReportDetail struct {
	Report Report
	PhotoSet PhotoSet
}
```

Assessment 使用 Foundation `domain.QualityEvaluation`，不得在 `assessment.go` 重复声明。

`ValidateReportDraft` 必须精确执行：Finding 数量 3–6、position 从 1 连续递增、priority 只允许 1–3、四类枚举、字符串 trim 后非空且有长度上限、source role 存在、anchor 的 `x/y` 在 `[0,1]`、`w/h` 在 `(0,1]` 且 `x+w <= 1`、`y+h <= 1`，并拒绝 `颜值|身材分|评分|百分位|缺陷严重|诊断|疾病|族裔|性格`。

- [ ] **Step 4: 运行领域测试**

Run: `cd apps/server && go test ./internal/service/assessment -run 'TestValidateSlots|TestPhotoSetContentHash|TestValidateReportDraft' -v`

Expected: PASS，三个测试组均通过。

- [ ] **Step 5: 提交领域边界**

```bash
git add apps/server/internal/domain/assessment.go apps/server/internal/service/assessment/validation.go apps/server/internal/service/assessment/validation_test.go
git commit -m "feat(server): define assessment report domain"
```

### Task 2: Baseline Schema 与数据库不变量

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Create: `apps/server/internal/repository/postgres/assessment_schema_test.go`

**Interfaces:**
- Consumes: 基础计划 baseline 中的 `users`、`user_profiles`、`media_assets`、`operations` 和 `provider_invocations`。
- Produces: `photo_sets`、`photo_set_items`、`analysis_runs`、`quality_evaluations`、`reports`、`report_findings` 及全部复合租户外键、幂等索引和不可变触发器。

- [ ] **Step 1: 写真实 PostgreSQL 约束测试**

```go
func TestAssessmentSchemaRejectsCrossTenantAndMutations(t *testing.T) {
	db := testutil.NewPostgres(t)
	alice, bob := seedUsers(t, db)
	aliceAssets := seedPhotoAssets(t, db, alice)
	photoSetID := insertPhotoSet(t, db, alice, aliceAssets)

	_, err := db.Exec(context.Background(), `
		INSERT INTO photo_set_items(id,user_id,photo_set_id,role,media_asset_id)
		VALUES(gen_random_uuid(),$1,$2,'face',$3)`,
		bob, photoSetID, aliceAssets.FaceAssetID)
	assertConstraint(t, err, "photo_set_items_photo_set_owner_fk")

	reportID := insertPublishedReport(t, db, alice, photoSetID, aliceAssets.BodyAssetID)
	_, err = db.Exec(context.Background(), `UPDATE reports SET priority_title='changed' WHERE id=$1`, reportID)
	assertImmutable(t, err)
	_, err = db.Exec(context.Background(), `DELETE FROM report_findings WHERE report_id=$1`, reportID)
	assertImmutable(t, err)
}

func TestPhotoSetAndAnalysisInputHashesAreIdempotentPerUser(t *testing.T) {
	db := testutil.NewPostgres(t)
	userID := seedUser(t, db)
	assertUniqueConstraint(t, db, `photo_sets`, userID, "content_hash", strings.Repeat("a", 64))
	assertUniqueConstraint(t, db, `analysis_runs`, userID, "input_hash", strings.Repeat("b", 64))
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `docker compose up -d postgres && cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestAssessmentSchema' -v`

Expected: FAIL，迁移后不存在 `photo_sets` 或约束名不匹配。

- [ ] **Step 3: 用精确 DDL 替换 baseline 的 Assessment 骨架**

基础计划负责创建唯一 baseline 文件和全表清单；本步骤把其中 `photo_sets` 至 `report_findings` 的骨架替换为下列完整定义。每张表在文件中只出现一次，不使用 `ALTER` 兼容旧库：

```sql
CREATE TABLE photo_sets (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  profile_snapshot jsonb NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  schema_version text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, content_hash)
);

CREATE TABLE photo_set_items (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL,
  photo_set_id uuid NOT NULL,
  role text NOT NULL CHECK (role IN ('face','side','body')),
  media_asset_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, photo_set_id, role),
  UNIQUE (user_id, photo_set_id, media_asset_id),
  CONSTRAINT photo_set_items_photo_set_owner_fk
    FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id) ON DELETE CASCADE,
  CONSTRAINT photo_set_items_asset_owner_fk
    FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id)
);

CREATE TABLE analysis_runs (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL,
  photo_set_id uuid NOT NULL,
  operation_id uuid NOT NULL,
  input_hash text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  profile_snapshot jsonb NOT NULL,
  analyzer_schema_version text NOT NULL,
  quality_policy_version text NOT NULL,
  provider_invocation_id uuid,
  outcome text CHECK (outcome IN ('published','rejected','failed')),
  report_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE (user_id, id),
  UNIQUE (user_id, input_hash),
  FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id),
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id)
);

CREATE TABLE quality_evaluations (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL,
  subject_type text NOT NULL CHECK (subject_type IN ('report','plan_set','render_candidate')),
  subject_id uuid NOT NULL,
  policy_version text NOT NULL,
  decision text NOT NULL CHECK (decision IN ('pass','retry','reject','error')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  internal_scores jsonb NOT NULL DEFAULT '{}',
  evaluator_invocation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, evaluator_invocation_id) REFERENCES provider_invocations(user_id, id)
);

CREATE TABLE reports (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL,
  photo_set_id uuid NOT NULL,
  profile_snapshot jsonb NOT NULL,
  impression_tags text[] NOT NULL,
  priority_title text NOT NULL,
  priority_copy text NOT NULL,
  hero_asset_id uuid NOT NULL,
  schema_version text NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  provider_invocation_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, content_hash),
  FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id),
  FOREIGN KEY (user_id, hero_asset_id) REFERENCES media_assets(user_id, id),
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id),
  FOREIGN KEY (user_id, quality_evaluation_id) REFERENCES quality_evaluations(user_id, id)
);

CREATE TABLE report_findings (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL,
  report_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit','color')),
  priority smallint NOT NULL CHECK (priority BETWEEN 1 AND 3),
  label text NOT NULL,
  visible_observation text NOT NULL,
  recommendation text NOT NULL,
  source_photo_item_id uuid NOT NULL,
  anchor_x double precision NOT NULL CHECK (anchor_x BETWEEN 0 AND 1),
  anchor_y double precision NOT NULL CHECK (anchor_y BETWEEN 0 AND 1),
  anchor_w double precision NOT NULL CHECK (anchor_w > 0 AND anchor_w <= 1 AND anchor_x + anchor_w <= 1),
  anchor_h double precision NOT NULL CHECK (anchor_h > 0 AND anchor_h <= 1 AND anchor_y + anchor_h <= 1),
  confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  position smallint NOT NULL CHECK (position > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, report_id, position),
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, source_photo_item_id) REFERENCES photo_set_items(user_id, id)
);

ALTER TABLE analysis_runs
  ADD CONSTRAINT analysis_runs_report_owner_fk
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id);

CREATE FUNCTION forbid_immutable_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'immutable relation % cannot be changed', TG_TABLE_NAME
    USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER reports_immutable BEFORE UPDATE ON reports
FOR EACH ROW EXECUTE FUNCTION forbid_immutable_mutation();
CREATE TRIGGER report_findings_immutable BEFORE UPDATE ON report_findings
FOR EACH ROW EXECUTE FUNCTION forbid_immutable_mutation();
CREATE TRIGGER quality_evaluations_immutable BEFORE UPDATE ON quality_evaluations
FOR EACH ROW EXECUTE FUNCTION forbid_immutable_mutation();
CREATE TRIGGER photo_sets_immutable BEFORE UPDATE ON photo_sets
FOR EACH ROW EXECUTE FUNCTION forbid_immutable_mutation();
CREATE TRIGGER photo_set_items_immutable BEFORE UPDATE ON photo_set_items
FOR EACH ROW EXECUTE FUNCTION forbid_immutable_mutation();
```

`photo_set_items` 明确增加 `id`，因为设计中的 `report_findings.source_photo_item_id` 需要稳定外键；`analysis_runs.profile_snapshot` 明确增加，满足“每次分析保存实际使用资料快照”的复现要求，而 PhotoSet 复用不应冻结后续分析的资料。

- [ ] **Step 4: 运行约束测试**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestAssessmentSchema|TestPhotoSetAndAnalysisInputHashes' -v`

Expected: PASS；跨租户插入报目标复合外键，Report/Finding 更新删除报 immutable，重复 hash 报唯一约束。

- [ ] **Step 5: 提交数据库边界**

```bash
git add apps/server/internal/database/migrations/001_baseline.sql apps/server/internal/repository/postgres/assessment_schema_test.go
git commit -m "feat(server): add immutable assessment schema"
```

### Task 3: Assessment PostgreSQL Adapter

**Files:**
- Create: `apps/server/internal/repository/postgres/assessment.go`
- Test: `apps/server/internal/repository/postgres/assessment_test.go`

**Interfaces:**
- Consumes: Task 1 领域类型和 Task 2 schema。
- Produces: `assessment.Repository` 所需的 `CreateOrReuseAssessment`、`GetRunInput`、`PrepareReport`、`CommitAssessment`、`FinishRunFailure`、`GetReport`、`GetCurrentReport`。

- [ ] **Step 1: 写幂等、租户和原子发布集成测试**

```go
func TestCreateOrReuseAssessmentReturnsSameRunAndOperation(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	first, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	require.NoError(t, err)
	second, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	require.NoError(t, err)
	require.Equal(t, first.Run.ID, second.Run.ID)
	require.Equal(t, first.Operation.ID, second.Operation.ID)
	require.Equal(t, first.Task.ID, second.Task.ID)
	require.Equal(t, 1, countRows(t, store.pool, "analysis_runs"))
}

func TestPrepareAndCommitReportAreTenantSafeAndLeaseGuarded(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, _ := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	input := validPublishParams(created.Run, fixture)
	input.Report.UserID = fixture.otherUserID
	_, err := store.PrepareReport(ctx, input)
	require.Error(t, err)
	require.Equal(t, 0, countRows(t, store.pool, "reports"))
	require.Nil(t, currentReportID(t, store.pool, fixture.userID))

	input = validPublishParams(created.Run, fixture)
	reportID, err := store.PrepareReport(ctx, input)
	require.NoError(t, err)
	_, err = store.GetReport(ctx, fixture.userID, reportID)
	require.ErrorIs(t, err, repository.ErrNotFound)
	lease := claimAssessmentTask(t, store, "worker-1")
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{ResultType:"report", ResultID:reportID})
	require.NoError(t, err)
	require.Equal(t, domain.CommitApplied, outcome)
	require.Equal(t, reportID, *currentReportID(t, store.pool, fixture.userID))
}

func TestGetReportUsesUserIDAndReturnsEvidenceAssets(t *testing.T) {
	store, fixture := publishedAssessmentStore(t)
	_, err := store.GetReport(ctx, fixture.otherUserID, fixture.reportID)
	require.ErrorIs(t, err, repository.ErrNotFound)
	got, err := store.GetReport(ctx, fixture.userID, fixture.reportID)
	require.NoError(t, err)
	require.Len(t, got.Findings, 3)
	require.Len(t, got.PhotoSet.Items, 3)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestCreateOrReuseAssessment|TestPrepareAndCommitReport|TestGetReportUsesUserID' -v`

Expected: FAIL，`CreateOrReuseAssessment` 等方法未定义。

- [ ] **Step 3: 实现事务方法**

```go
type CreateAssessmentParams struct {
	UserID, PhotoSetSchemaVersion string
	PhotoSetContentHash, AnalysisInputHash string
	ProfileSnapshot json.RawMessage
	Slots domain.PhotoSlots
	Assets map[domain.PhotoRole]domain.MediaAsset
	AnalyzerSchemaVersion, QualityPolicyVersion string
	MaxTaskAttempts int
}

type CreatedAssessment struct {
	PhotoSet domain.PhotoSet
	Run domain.AnalysisRun
	Operation domain.Operation
	Task domain.Task
	Reused bool
}

type PrepareReportParams struct {
	RunID, UserID string
	Report domain.Report
	Quality domain.QualityEvaluation
	Findings []domain.ReportFinding
}
```

`CreateOrReuseAssessment` 必须在一个事务中：

1. `INSERT ... ON CONFLICT (user_id, content_hash) DO NOTHING` 创建/复用 PhotoSet。
2. 仅新 PhotoSet 插入三条带稳定 UUID 的 item。
3. 先按 `(user_id,input_hash)` 查询 AnalysisRun；存在时返回其 Operation 和唯一 active Task。
4. 不存在时创建 accepted Operation、AnalysisRun、`assessment` Task，`dedupe_key='assessment:'+input_hash`，`max_attempts` 从 Registry 的 `Definition(domain.TaskType("assessment"))` 读取。

HTTP `Idempotency-Key` 的占用、冲突和响应重放全部由基础计划 `httpapi.requireIdempotency` 负责；Repository 只以 `analysis_runs.input_hash` 和 Task dedupe key 防止业务层重复副作用，不建立第二套幂等表或 key 解析。

`PrepareReport` 只在 evidence Gate 通过后调用，并在一个事务中幂等插入 QualityEvaluation、Report 和全部 Finding；`reports.content_hash` 冲突时返回同一 Report ID。`GetReport/GetCurrentReport` 必须 join `analysis_runs.outcome='published' AND analysis_runs.report_id=reports.id`，所以已准备但尚未 Commit 的报告不可通过 API 读取。

`CommitAssessment` 必须在一个事务中校验 `TaskLease` 的 `(task_id,lease_token,status='leased')`，把 AnalysisRun 更新为 `published/report_id`，仅当当前报告更旧时更新 `user_profiles.current_report_id/version`，并把 Task/Operation 更新为 succeeded/result=report。旧运行晚完成不得覆盖新报告指针；比较 `(reports.created_at,reports.id)`。lease 失效返回 `domain.CommitSuperseded`，不修改 Run、指针、Task 或 Operation。

- [ ] **Step 4: 运行 Adapter 测试**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestCreateOrReuseAssessment|TestPrepareAndCommitReport|TestGetReportUsesUserID' -v`

Expected: PASS；重复请求只有一组 Run/Operation/Task，越权统一 `ErrNotFound`，未 Commit 报告不可读，失效 lease 不能推进当前报告。

- [ ] **Step 5: 提交 Adapter**

```bash
git add apps/server/internal/repository/postgres/assessment.go apps/server/internal/repository/postgres/assessment_test.go
git commit -m "feat(server): persist assessment reports atomically"
```

### Task 4: 照片技术检查与标准化失败原因

**Files:**
- Create: `apps/server/internal/service/assessment/photo_technical.go`
- Test: `apps/server/internal/service/assessment/photo_technical_test.go`
- Create: `apps/server/internal/service/assessment/testdata/images/generate_test.go`

**Interfaces:**
- Consumes: `assessment.ImageLoader.Load(ctx, []domain.PhotoSetItem) ([]provider.ImageInput, error)`。
- Produces: `TechnicalPhotoChecker.Check([]provider.ImageInput) error` 和不泄露内部细节的 `PhotoRejectedError`。

- [ ] **Step 1: 写 JPEG/PNG、魔数、解码与压缩炸弹测试**

```go
func TestTechnicalPhotoChecker(t *testing.T) {
	validJPEG := encodedImage(t, "jpeg", 1200, 1600)
	validPNG := encodedImage(t, "png", 1200, 1600)
	tests := []struct{
		name, declaredMIME, code string
		data []byte
	}{
		{"jpeg", "image/jpeg", "", validJPEG},
		{"png", "image/png", "", validPNG},
		{"mime mismatch", "image/png", "photo_mime_mismatch", validJPEG},
		{"webp", "image/webp", "photo_format_unsupported", []byte("RIFFxxxxWEBP")},
		{"truncated", "image/jpeg", "photo_decode_failed", validJPEG[:64]},
		{"too many pixels", "image/png", "photo_dimensions_exceeded", pngHeader(10000, 10000)},
	}
	for _, tc := range tests {
		err := (TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 8192, MaxPixels: 40_000_000}).CheckOne(
			provider.ImageInput{Role:"face", MIMEType:tc.declaredMIME, Data:tc.data},
		)
		assertPhotoCode(t, err, tc.code)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/service/assessment -run TestTechnicalPhotoChecker -v`

Expected: FAIL，`TechnicalPhotoChecker` 未定义。

- [ ] **Step 3: 使用标准库实现先读头、后解码**

```go
func (c TechnicalPhotoChecker) CheckOne(input provider.ImageInput) error {
	if len(input.Data) == 0 || int64(len(input.Data)) > c.MaxBytes {
		return reject(input.Role, "photo_size_invalid")
	}
	detected := http.DetectContentType(input.Data)
	if detected != "image/jpeg" && detected != "image/png" {
		return reject(input.Role, "photo_format_unsupported")
	}
	if detected != input.MIMEType {
		return reject(input.Role, "photo_mime_mismatch")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(input.Data))
	if err != nil || "image/"+format != detected {
		return reject(input.Role, "photo_decode_failed")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > c.MaxDimension || cfg.Height > c.MaxDimension ||
		int64(cfg.Width)*int64(cfg.Height) > c.MaxPixels {
		return reject(input.Role, "photo_dimensions_exceeded")
	}
	if _, _, err := image.Decode(bytes.NewReader(input.Data)); err != nil {
		return reject(input.Role, "photo_decode_failed")
	}
	return nil
}
```

对外错误只映射为：

```go
var photoPublicMessage = map[string]string{
	"photo_size_invalid": "照片文件过大或为空，请重新选择一张照片",
	"photo_format_unsupported": "仅支持 JPEG 或 PNG，请更换照片后重试",
	"photo_mime_mismatch": "照片格式无法确认，请重新选择原始照片",
	"photo_decode_failed": "照片文件无法读取，请重新拍摄或更换照片",
	"photo_dimensions_exceeded": "照片尺寸过大，请压缩后重新上传",
}
```

- [ ] **Step 4: 运行技术检查测试**

Run: `cd apps/server && go test ./internal/service/assessment -run TestTechnicalPhotoChecker -v`

Expected: PASS；测试进程无大内存分配，WebP 和超大像素在完整解码前被拒绝。

- [ ] **Step 5: 提交技术门禁**

```bash
git add apps/server/internal/service/assessment/photo_technical.go apps/server/internal/service/assessment/photo_technical_test.go apps/server/internal/service/assessment/testdata/images/generate_test.go
git commit -m "feat(server): validate assessment image bytes"
```

### Task 5: 四个 AI 能力的结构化 Provider 契约

**Files:**
- Create: `apps/server/internal/provider/ai/assessment.go`
- Create: `apps/server/internal/provider/ai/assessment_schema.go`
- Test: `apps/server/internal/provider/ai/assessment_test.go`
- Modify: `apps/server/internal/provider/ai/contracts.go`
- Modify: `apps/server/internal/config/ai_routing.go`
- Modify: `apps/server/config/ai-routing.example.json`
- Modify: `apps/server/config/ai-routing.production.json`

**Interfaces:**
- Consumes: 基础 `ai.Runtime.Structured` 和 `InvocationMeta`。
- Produces: `PhotoContentChecker.Check`、`IdentityChecker.Check`、`ReportAnalyzer.Analyze`、`EvidenceVerifier.Verify`。

- [ ] **Step 1: 写 Provider 契约测试**

```go
func TestAssessmentProvidersKeepImageOrderAndInternalConfidence(t *testing.T) {
	runtime := &runtimeSpy{responses: map[string][]byte{
		"photo_quality_check": []byte(`{"photos":[{"role":"face","decision":"pass","reason_code":""},{"role":"side","decision":"pass","reason_code":""},{"role":"body","decision":"pass","reason_code":""}]}`),
		"photo_identity_consistency": []byte(`{"decision":"pass","confidence":0.97}`),
	}}
	providers := NewAssessmentProviders(runtime)
	images := orderedImages()
	quality, err := providers.PhotoContent.Check(ctx, images)
	require.NoError(t, err)
	require.Equal(t, []string{"face","side","body"}, runtime.roles("photo_quality_check"))
	require.Len(t, quality.Photos, 3)
	identity, err := providers.Identity.Check(ctx, images)
	require.NoError(t, err)
	require.InDelta(t, .97, identity.Confidence, .001)
}

func TestReportSchemaRejectsVisualScoresAndMissingAnchor(t *testing.T) {
	payload := validReportJSON()
	payload.Findings[0].VisibleObservation = "身材评分 80"
	require.Error(t, validateReportPayload(marshal(t, payload)))
	payload = validReportJSON()
	payload.Findings[0].Anchor.W = 0
	require.Error(t, validateReportPayload(marshal(t, payload)))
}

func TestEvidenceVerifierRequiresOneDecisionPerFinding(t *testing.T) {
	require.Error(t, validateEvidencePayload(
		[]string{"f1","f2"},
		[]byte(`{"findings":[{"key":"f1","supported":true,"confidence":0.96,"reason_code":""}]}`),
	))
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/provider/ai -run 'TestAssessmentProviders|TestReportSchema|TestEvidenceVerifier' -v`

Expected: FAIL，四个 Provider 和 schema validator 未定义。

- [ ] **Step 3: 定义窄 Provider DTO**

```go
type PhotoQualityItem struct {
	Role domain.PhotoRole `json:"role"`
	Decision string `json:"decision"` // pass|reject
	ReasonCode string `json:"reason_code"`
}
type PhotoQualityResult struct {
	Photos []PhotoQualityItem
	Meta InvocationMeta
}
type IdentityResult struct {
	Decision string `json:"decision"` // pass|reject|uncertain
	Confidence float64 `json:"confidence"`
	Meta InvocationMeta `json:"-"`
}
type ReportAnalysisResult struct {
	Draft domain.ReportDraft
	Meta InvocationMeta
}
type EvidenceDecision struct {
	Key string `json:"key"`
	Supported bool `json:"supported"`
	Confidence float64 `json:"confidence"`
	ReasonCode string `json:"reason_code"`
}
type EvidenceResult struct {
	Findings []EvidenceDecision
	Meta InvocationMeta
}
```

`photo_quality_check` reason code 只允许 `multiple_people | no_person | screenshot | illustration | pet | face_not_frontal | face_occluded | side_not_profile | body_not_head_to_calf | too_blurry | too_dark`。`appearance_analysis` prompt 明确禁止外貌/身材/年龄/敏感属性评分，明确要求职业、预算、身高只能影响 recommendation。`report_evidence_verification` 必须使用三张原图和 draft finding，不接收上一模型的自由文本解释。

路由校验必须把四个 capability 归入结构化多图能力，并要求 `max_input_images >= 3`；生产配置缺任一 capability 时启动失败，不静默跳过 Gate。Demo 路由只能由显式 Demo Asset 路径选择，不能成为生产 fallback。

- [ ] **Step 4: 运行 Provider 契约和配置测试**

Run: `cd apps/server && go test ./internal/provider/ai ./internal/config -run 'TestAssessment|TestReport|TestEvidence|TestAIRouting' -v`

Expected: PASS；请求图片顺序固定 face/side/body，非法枚举/缺项/评分词被拒，四条生产路由均有三图能力。

- [ ] **Step 5: 提交 Provider 契约**

```bash
git add apps/server/internal/provider/ai/assessment.go apps/server/internal/provider/ai/assessment_schema.go apps/server/internal/provider/ai/assessment_test.go apps/server/internal/provider/ai/contracts.go apps/server/internal/config/ai_routing.go apps/server/config/ai-routing.example.json apps/server/config/ai-routing.production.json
git commit -m "feat(server): add assessment AI capability contracts"
```

### Task 6: 创建 Assessment 用例与三槽位幂等

**Files:**
- Create: `apps/server/internal/service/assessment/ports.go`
- Create: `apps/server/internal/service/assessment/service.go`
- Test: `apps/server/internal/service/assessment/service_test.go`

**Interfaces:**
- Consumes: `AssetReader.GetReadyAssets(ctx,userID,ids)`、`ProfileReader.Snapshot(ctx,userID)`、Task 3 Repository。
- Produces: `Service.Create(ctx, CreateCommand) (CreateResult,error)`、`Service.GetReport`、`Service.GetCurrentReport`。

- [ ] **Step 1: 写创建、混合来源和 hash 幂等测试**

```go
func TestCreateBuildsRoleAddressedPhotoSet(t *testing.T) {
	repo := newRepoFake()
	svc := newService(repo, validAssetReader())
	got, err := svc.Create(ctx, CreateCommand{
		UserID:"user-1",
		Slots:domain.PhotoSlots{FaceAssetID:"face", SideAssetID:"side", BodyAssetID:"body"},
	})
	require.NoError(t, err)
	require.Equal(t, "assessment", string(got.Operation.Kind))
	require.Equal(t, domain.TaskType("assessment"), got.Task.Type)
	require.Equal(t, "face", repo.last.Assets[domain.PhotoRoleFace].ID)
}

func TestCreateRejectsMixedDemoAndUserPhotos(t *testing.T) {
	reader := validAssetReader()
	reader.assets["side"] = demoAsset("side")
	_, err := newService(newRepoFake(), reader).Create(ctx, validCreateCommand())
	assertValidationCode(t, err, "photo_origin_mixed")
}

func TestCreateSameInputsReusesRun(t *testing.T) {
	repo := newRepoFake()
	svc := newService(repo, validAssetReader())
	first, _ := svc.Create(ctx, validCreateCommand())
	second, _ := svc.Create(ctx, validCreateCommand())
	require.Equal(t, first.Run.ID, second.Run.ID)
	require.Equal(t, 1, repo.createCount)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/service/assessment -run 'TestCreateBuilds|TestCreateRejects|TestCreateSameInputs' -v`

Expected: FAIL，`Service.Create` 和 ports 未定义。

- [ ] **Step 3: 实现创建用例**

```go
type CreateCommand struct {
	UserID string
	Slots domain.PhotoSlots
}

type CreateResult struct {
	Run domain.AnalysisRun
	PhotoSet domain.PhotoSet
	Operation domain.Operation
	Task domain.Task
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (CreateResult, error) {
	ids := []string{cmd.Slots.FaceAssetID, cmd.Slots.SideAssetID, cmd.Slots.BodyAssetID}
	assets, err := s.assets.GetReadyAssets(ctx, cmd.UserID, ids)
	if err != nil { return CreateResult{}, hideForeignAsset(err) }
	byID := indexAssets(assets)
	if err := ValidateSlots(cmd.UserID, cmd.Slots, byID); err != nil { return CreateResult{}, err }
	if err := validateOrigins(byID); err != nil { return CreateResult{}, err }
	profile, err := s.profiles.Snapshot(ctx, cmd.UserID)
	if err != nil { return CreateResult{}, err }
	contentHash := PhotoSetContentHash(PhotoSetSchemaVersion, roleHashes(cmd.Slots, byID))
	inputHash := AnalysisInputHash(contentHash, profile, AnalyzerSchemaVersion, QualityPolicyVersion)
	params := buildCreateParams(cmd, byID, profile, contentHash, inputHash)
	params.MaxTaskAttempts = s.taskDefinition.MaxAttempts
	return s.repo.CreateOrReuseAssessment(ctx, params)
}
```

`GetReport`/`GetCurrentReport` 只读取已发布 Report；Repository 返回的 object key 必须通过 `MediaPresenter` 转成 `source_kind/url/url_expires_at/display_label`，不得把 object key 或 Provider 字段放进 DTO。

`assessment.NewService` 必须接收 Bootstrap 创建的同一个 `taskrunner.Definition{Type:"assessment",MaxAttempts:3,Timeout:90*time.Second,LeaseDuration:30*time.Second,HeartbeatEvery:10*time.Second,Concurrency:2,...}`；Service 只读取该值的 `MaxAttempts`，不得复制重试常量。

- [ ] **Step 4: 运行 Service 创建测试**

Run: `cd apps/server && go test ./internal/service/assessment -run 'TestCreateBuilds|TestCreateRejects|TestCreateSameInputs' -v`

Expected: PASS；槽位不依赖数组位置，混合来源失败，重复输入复用。

- [ ] **Step 5: 提交创建用例**

```bash
git add apps/server/internal/service/assessment/ports.go apps/server/internal/service/assessment/service.go apps/server/internal/service/assessment/service_test.go
git commit -m "feat(server): create idempotent assessments"
```

### Task 7: Assessment Task 编排、证据 Gate 与原子发布

**Files:**
- Create: `apps/server/internal/service/assessment/handler.go`
- Create: `apps/server/internal/service/assessment/policy.go`
- Test: `apps/server/internal/service/assessment/handler_test.go`

**Interfaces:**
- Consumes: Task 4 TechnicalPhotoChecker、Task 5 四个 Provider、Task 3 `PrepareReport/CommitAssessment/FinishRunFailure`、基础 `OperationProgress`。
- Produces: 实现基础计划 `taskrunner.Handler` 的 `assessment.Handler`，Task type 固定 `assessment`，payload version 固定 `1`。

- [ ] **Step 1: 写门禁顺序、补生成一次和失败关闭测试**

```go
func TestHandlerRunsGatesInOrderAndPublishesSupportedFindings(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.result = EvidenceResult{Findings: []EvidenceDecision{
		{Key:"f1", Supported:true, Confidence:.98},
		{Key:"f2", Supported:false, Confidence:.91, ReasonCode:"observation_not_visible"},
		{Key:"f3", Supported:true, Confidence:.97},
		{Key:"f4", Supported:true, Confidence:.96},
	}}
	result, err := spy.handler.Execute(ctx, spy.lease)
	require.NoError(t, err)
	require.Equal(t, []string{"load","technical","content","identity","analyze","evidence","prepare"}, spy.calls)
	require.Equal(t, "report", result.ResultType)
	require.Len(t, spy.repo.prepared.Findings, 3)
	outcome, err := spy.handler.Commit(ctx, spy.lease, result)
	require.NoError(t, err)
	require.Equal(t, domain.CommitApplied, outcome)
}

func TestHandlerRegeneratesOnceWhenEvidenceLeavesFewerThanThree(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.results = []EvidenceResult{onlyTwoSupported(), threeSupported()}
	_, err := spy.handler.Execute(ctx, spy.lease)
	require.NoError(t, err)
	require.Equal(t, 2, spy.analyzer.calls)
	require.Equal(t, 2, spy.evidence.calls)
}

func TestHandlerRejectsIdentityUncertaintyWithoutAnalysis(t *testing.T) {
	spy := newHandlerFixture()
	spy.identity.result = IdentityResult{Decision:"uncertain", Confidence:.71}
	_, err := spy.handler.Execute(ctx, spy.lease)
	assertPublicFailure(t, err, "photo_identity_uncertain", "照片差异较大，请重新拍摄确认", false)
	require.Equal(t, 0, spy.analyzer.calls)
	require.Equal(t, domain.AnalysisOutcomeRejected, spy.repo.failure.Outcome)
}

func TestHandlerFailsClosedAfterSecondEvidenceFailure(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.results = []EvidenceResult{onlyTwoSupported(), onlyTwoSupported()}
	_, err := spy.handler.Execute(ctx, spy.lease)
	assertPublicFailure(t, err, "report_evidence_insufficient", "这次未能形成可靠报告，请重新拍摄后再试", true)
	require.Equal(t, 0, spy.repo.publishCount)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/service/assessment -run TestHandler -v`

Expected: FAIL，`Handler`、Gate policy 和 typed public failure 未定义。

- [ ] **Step 3: 实现单 Task 有界编排**

```go
func (h *Handler) Type() domain.TaskType { return domain.TaskType("assessment") }

func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	task := lease.Task
	input, err := h.repo.GetRunInput(ctx, task.UserID, task.SubjectID)
	if err != nil { return domain.TaskResult{}, err }
	images, err := h.images.Load(ctx, input.PhotoSet.Items)
	if err != nil { return h.stageFailure(lease, domain.AnalysisOutcomeFailed, classifyLoadError(err)) }
	if err := h.progress.Set(ctx, task.OperationID, 1000, StagePhotoTechnicalCheck); err != nil { return domain.TaskResult{}, err }
	if err := h.technical.Check(images); err != nil { return h.stageRejection(lease, err) }
	if err := h.progress.Set(ctx, task.OperationID, 2500, StagePhotoContentCheck); err != nil { return domain.TaskResult{}, err }
	content, err := h.photoContent.Check(ctx, images)
	if err != nil { return domain.TaskResult{}, err }
	if err := h.policy.AcceptContent(content); err != nil { return h.stageRejection(lease, err) }
	if err := h.progress.Set(ctx, task.OperationID, 4000, StagePhotoIdentityCheck); err != nil { return domain.TaskResult{}, err }
	identity, err := h.identity.Check(ctx, images)
	if err != nil { return domain.TaskResult{}, err }
	if identity.Decision != "pass" || identity.Confidence < .92 {
		return h.stageRejection(lease, NewPublicFailure("photo_identity_uncertain", "照片差异较大，请重新拍摄确认", false))
	}
	for generation := 1; generation <= 2; generation++ {
		_ = h.progress.Set(ctx, task.OperationID, 5500, StageReportGenerating)
		analysisResult, err := h.analyzer.Analyze(ctx, buildAnalysisInput(input, images, generation))
		if err != nil { return domain.TaskResult{}, err }
		draft := analysisResult.Draft
		if err := ValidateReportDraft(draft); err != nil { return domain.TaskResult{}, err }
		_ = h.progress.Set(ctx, task.OperationID, 8000, StageEvidenceVerifying)
		evidence, err := h.evidence.Verify(ctx, images, draft)
		if err != nil { return domain.TaskResult{}, err }
		supported := h.policy.SupportedFindings(draft.Findings, evidence, .95)
		if len(supported) < 3 { continue }
		_ = h.progress.Set(ctx, task.OperationID, 9500, StageReportPublishing)
		report, quality := buildImmutablePublication(input, draft, evidence, supported, analysisResult.Meta.InvocationID)
		reportID, err := h.repo.PrepareReport(ctx, PrepareReportParams{
			RunID:input.Run.ID, UserID:task.UserID,
			Report:report, Quality:quality, Findings:report.Findings,
		})
		if err != nil { return domain.TaskResult{}, err }
		return domain.TaskResult{ResultType:"report", ResultID:reportID}, nil
	}
	return h.stageRejection(lease, NewPublicFailure("report_evidence_insufficient", "这次未能形成可靠报告，请重新拍摄后再试", true))
}

func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	return h.repo.CommitAssessment(ctx, lease, result)
}
```

Gate 规则固定为：evidence `supported=true` 且内部 confidence 达到 `quality_policy_version` 对应的金集校准阈值才保留；不支持 Finding 直接删除；少于 3 条时携带 reason codes 补生成一次；第二轮仍少于 3 条则 AnalysisRun=`rejected`、Operation=`failed`，不插入 Report。校准程序选择能让证据 precision ≥95% 且 recall ≥80% 的最低阈值。Task 网络重试不增加两轮质量预算。`Execute` 不更新 Task/Operation 终态或当前报告指针；这些动作只在 `Commit` 的 lease CAS 事务发生。

- [ ] **Step 4: 运行编排测试**

Run: `cd apps/server && go test ./internal/service/assessment -run TestHandler -v`

Expected: PASS；调用顺序固定、最多两轮报告、失败无 Report、confidence 不进入公开结果。

- [ ] **Step 5: 提交编排与 Gate**

```bash
git add apps/server/internal/service/assessment/handler.go apps/server/internal/service/assessment/policy.go apps/server/internal/service/assessment/handler_test.go
git commit -m "feat(server): gate and publish evidence-backed reports"
```

### Task 8: Registry 与 API/Worker 装配

**Files:**
- Modify: `apps/server/internal/bootstrap/api.go`
- Modify: `apps/server/internal/bootstrap/worker.go`
- Test: `apps/server/internal/bootstrap/assessment_test.go`

**Interfaces:**
- Consumes: `assessment.NewService`、`assessment.NewHandler` 和基础 Registry。
- Produces: API 注入 Assessment Service；Worker 注册且仅注册一次 `assessment`。

- [ ] **Step 1: 写装配测试**

```go
func TestWorkerRegistersAssessmentHandlerWithoutChangingRunnerLoop(t *testing.T) {
	bundle := buildWorkerForTest(t)
	require.NotNil(t, bundle.Assessment)
	require.Equal(t, []domain.TaskType{"assessment"}, bundle.Registry.Types())
	definition, ok := bundle.Registry.Definition(domain.TaskType("assessment"))
	require.True(t, ok)
	require.Equal(t, 3, definition.MaxAttempts)
}

func TestProductionBootstrapRequiresAllAssessmentCapabilities(t *testing.T) {
	cfg := validProductionConfig()
	delete(cfg.AIRouting.Routes, "report_evidence_verification")
	_, err := BuildWorker(cfg)
	require.ErrorContains(t, err, "report_evidence_verification")
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/bootstrap -run 'TestWorkerRegistersAssessment|TestProductionBootstrap' -v`

Expected: FAIL，Bootstrap 尚未构造 Assessment 依赖。

- [ ] **Step 3: 装配窄依赖**

Bootstrap 先构造唯一的 `assessmentDefinition`，同时传给 API 侧 `assessment.NewService` 和 Worker 侧 `taskrunner.NewRegistry(definitions,handlers)`。Worker 创建同一 Repository adapter、Media loader、四个 Provider 和 `assessment.Handler`，再把 definition 与 handler 一次性交给 `NewRegistry`；不得增加运行期 `Register`、不得向 Runner 添加 `switch task.Type`，不得把 ProviderOptions 参数袋重新引入。

- [ ] **Step 4: 运行装配和 Task runner 测试**

Run: `cd apps/server && go test ./internal/bootstrap ./internal/service/taskrunner -run 'TestWorkerRegistersAssessment|TestProductionBootstrap|TestRegistry' -v`

Expected: PASS；Registry 有一个 assessment handler，缺 Gate route 的 production 启动失败。

- [ ] **Step 5: 提交装配**

```bash
git add apps/server/internal/bootstrap/api.go apps/server/internal/bootstrap/worker.go apps/server/internal/bootstrap/assessment_test.go
git commit -m "feat(server): wire assessment service and worker"
```

### Task 9: OpenAPI 与 HTTP Handler

**Files:**
- Modify: `contracts/openapi.yaml`
- Create: `apps/server/internal/httpapi/assessments.go`
- Create: `apps/server/internal/httpapi/reports.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Delete: `apps/server/internal/httpapi/analyses.go`
- Test: `apps/server/internal/httpapi/assessments_test.go`
- Test: `apps/server/internal/httpapi/reports_test.go`

**Interfaces:**
- Consumes: Task 6 Service。
- Produces: `POST /v1/assessments`、`GET /v1/reports/current`、`GET /v1/reports/{id}`；Operation 读取由基础计划提供。

- [ ] **Step 1: 写 HTTP 契约测试**

```go
func TestPostAssessmentRequiresIdempotencyAndNamedSlots(t *testing.T) {
	api := newAssessmentAPI(t)
	body := `{"photos":{"face_asset_id":"face","side_asset_id":"side","body_asset_id":"body"}}`
	res := api.Do(http.MethodPost, "/v1/assessments", body, nil)
	assertError(t, res, 400, "idempotency_key_required", false)

	res = api.Do(http.MethodPost, "/v1/assessments", body, map[string]string{"Idempotency-Key":"assessment-1"})
	assertStatus(t, res, 202)
	assertJSONPath(t, res, "operation.kind", "assessment")
	assertJSONAbsent(t, res, "task")
}

func TestReportResponseOmitsInternalFieldsAndIncludesEvidencePhoto(t *testing.T) {
	res := newReportAPI(t).Do(http.MethodGet, "/v1/reports/report-1", "", nil)
	assertStatus(t, res, 200)
	assertJSONPath(t, res, "data.findings.0.source_photo.role", "face")
	assertJSONPath(t, res, "data.findings.0.anchor.w", .2)
	for _, field := range []string{"confidence","provider_invocation_id","quality_evaluation_id","provider_version"} {
		assertJSONDoesNotContainKey(t, res, field)
	}
}

func TestReportCrossTenantIs404(t *testing.T) {
	res := newReportAPIAs(t, "other-user").Do(http.MethodGet, "/v1/reports/report-1", "", nil)
	assertError(t, res, 404, "not_found", false)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./internal/httpapi -run 'TestPostAssessment|TestReportResponse|TestReportCrossTenant' -v`

Expected: FAIL，路由返回 404 或 DTO 未定义。

- [ ] **Step 3: 定义不泄露内部字段的契约**

```yaml
/v1/assessments:
  post:
    operationId: createAssessment
    parameters:
      - $ref: '#/components/parameters/IdempotencyKey'
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/CreateAssessmentRequest'
    responses:
      '202':
        description: Assessment 已受理

CreateAssessmentRequest:
  type: object
  additionalProperties: false
  required: [photos]
  properties:
    photos:
      type: object
      additionalProperties: false
      required: [face_asset_id, side_asset_id, body_asset_id]
      properties:
        face_asset_id: { type: string, format: uuid }
        side_asset_id: { type: string, format: uuid }
        body_asset_id: { type: string, format: uuid }
```

公开 Report DTO 固定为：

```go
type ReportResponse struct {
	ID, PhotoSetID, HeroAssetID, PriorityTitle, PriorityCopy, SchemaVersion string
	ImpressionTags []string
	SourceMedia ReportSourceMediaResponse
	Findings []FindingResponse
	CreatedAt time.Time
}
type ReportPhotoResponse struct {
	ItemID string
	Role domain.PhotoRole
	Media MediaResponse
}
type ReportSourceMediaResponse struct {
	Face ReportPhotoResponse
	Side ReportPhotoResponse
	Body ReportPhotoResponse
}
type FindingResponse struct {
	ID, Category, Label, VisibleObservation, Recommendation string
	Priority, Position int
	SourcePhoto struct{ ItemID string; Role domain.PhotoRole }
	Anchor domain.EvidenceAnchor
}
```

`MediaResponse` 只返回 `asset_id/url/url_expires_at/mime_type/source_kind/display_label`。用户图 `source_kind=user_original`、`display_label=原本`；Demo `source_kind=demo_example`、`display_label=效果示例`。

删除旧 `/v1/analyses`、`/v1/analyses/current`、`/v1/analyses/{id}` 路由和 schema，不保留别名。错误映射必须包含 `retryable` 布尔值，且 404 不区分不存在与越权。

`POST /v1/assessments` 必须用基础计划的 `requireIdempotency` 包裹；handler 不解析、保存或自行重放 key。

- [ ] **Step 4: 运行 HTTP 与契约同步测试**

Run: `cd apps/server && go test ./internal/httpapi -run 'TestPostAssessment|TestReportResponse|TestReportCrossTenant' -v && cd ../.. && node contracts/scripts/check-sync.mjs`

Expected: PASS；202 顶层是 `data + operation`，没有 Task；报告无内部 confidence/provider 字段；OpenAPI 与 core 在 Task 10 前会因 core 路径未更新而明确 FAIL。该同步失败是本任务预期的跨任务红灯，不提交为可合并状态，紧接 Task 10 消除。

- [ ] **Step 5: 保持跨端原子提交边界**

本 Task 的 Go 测试已通过，但契约同步检查会在 Task 10 更新 core 前保持红灯，因此这里不创建中间提交。Task 9 与 Task 10 在 Task 10 Step 5 合并为一个原子提交，避免仓库出现 OpenAPI 与客户端不一致的提交点。

### Task 10: `@zsm/core` Assessment/Report 类型、端点与文案

**Files:**
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/api/endpoints.ts`
- Modify: `packages/core/src/copy/zh.ts`
- Create: `packages/core/src/report/view.ts`
- Modify: `packages/core/src/index.ts`
- Create: `packages/core/tests/assessment-report.test.mjs`

**Interfaces:**
- Consumes: Task 9 OpenAPI。
- Produces: `CreateAssessmentInput`、`AssessmentAccepted`、新 `Report/Finding/ReportPhoto`、`createAssessment`、`getReport`、`getCurrentReport`、`reportPhotoMap`、按 error code 的动作文案。

- [ ] **Step 1: 写端点路径、公开字段与纯映射测试**

```js
test('assessment endpoint sends named slots and idempotency key', async () => {
  const calls = []
  const api = createApiEndpoints(fakeClient(calls))
  await api.createAssessment(
    { photos: { face_asset_id: FACE, side_asset_id: SIDE, body_asset_id: BODY } },
    { idempotencyKey: 'assessment-1' },
  )
  assert.deepEqual(calls[0], {
    method: 'POST',
    path: '/v1/assessments',
    headers: { 'Idempotency-Key': 'assessment-1' },
    data: { photos: { face_asset_id: FACE, side_asset_id: SIDE, body_asset_id: BODY } },
  })
})

test('idempotency keys are injectable and bounded', () => {
  assert.equal(createIdempotencyKey('assessment', () => 1700000000000, () => 0.5), 'assessment-loyw3v28-i')
  assert.ok(createIdempotencyKey('assessment').length <= 128)
})

test('reportPhotoMap never substitutes a missing user photo', () => {
  const map = reportPhotoMap({
    source_media: {
      face: { item_id:'item-face', media:{ asset_id:FACE, url:'', mime_type:'image/jpeg', source_kind:'user_original', display_label:'原本' } },
      body: { item_id:'item-body', media:{ asset_id:BODY, url:'https://img/body.jpg', mime_type:'image/jpeg', source_kind:'user_original', display_label:'原本' } },
    },
  })
  assert.equal(map.face.url, '')
  assert.equal(map.body.url, 'https://img/body.jpg')
})

test('identity uncertainty copy gives one next action', () => {
  assert.deepEqual(ASSESSMENT_ERROR_COPY.photo_identity_uncertain, {
    message: '照片差异较大，请重新拍摄确认',
    action: '重新拍摄',
  })
})
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `pnpm --filter @zsm/core test`

Expected: FAIL，`createAssessment`、`reportPhotoMap` 和新文案未定义。

- [ ] **Step 3: 替换旧类型与端点**

```ts
export interface CreateAssessmentInput {
  photos: {
    face_asset_id: string
    side_asset_id: string
    body_asset_id: string
  }
}
export interface AssessmentAccepted {
  analysis_run_id: string
  photo_set_id: string
}
export interface ReportMedia {
  asset_id: string
  url: string
  url_expires_at: string
  source_kind: 'user_original' | 'demo_example'
  display_label: '原本' | '效果示例'
}
export interface ReportPhoto {
  item_id: string
  role: 'face' | 'side' | 'body'
  media: ReportMedia
}
export interface Finding {
  id: string
  category: 'hair' | 'makeup' | 'outfit' | 'color'
  priority: 1 | 2 | 3
  label: string
  visible_observation: string
  recommendation: string
  source_photo: { item_id: string; asset_id: string; role: 'face' | 'side' | 'body' }
  anchor: { x: number; y: number; w: number; h: number }
  position: number
}
```

删除 `Analysis`、`CreateAnalysisInput` 和旧 Report 字段。`API_PATHS` 删除三个 analyses 操作，加入 `assessments: 'POST /v1/assessments'`；创建端点必须显式接收 idempotency key，不能在 client 内随机生成后隐藏重放语义。`createIdempotencyKey(prefix, now = Date.now, random = Math.random)` 只生成不透明请求键，页面必须在一次提交重试期间复用同一个键。

- [ ] **Step 4: 运行 core、类型和契约同步检查**

Run: `pnpm --filter @zsm/core test && pnpm --filter @zsm/core typecheck && node contracts/scripts/check-sync.mjs`

Expected: PASS；同步检查输出“契约与 core 端点完全一致”。

- [ ] **Step 5: 提交 core 契约**

```bash
git add contracts/openapi.yaml \
  apps/server/internal/httpapi/assessments.go \
  apps/server/internal/httpapi/reports.go \
  apps/server/internal/httpapi/httpapi.go \
  apps/server/internal/httpapi/assessments_test.go \
  apps/server/internal/httpapi/reports_test.go \
  packages/core/src/types/index.ts \
  packages/core/src/api/endpoints.ts \
  packages/core/src/copy/zh.ts \
  packages/core/src/report/view.ts \
  packages/core/src/index.ts \
  packages/core/tests/assessment-report.test.mjs
git rm apps/server/internal/httpapi/analyses.go
git commit -m "feat(api): expose assessment report contract"
```

### Task 11: 小程序 Assessment 进度与报告页接线

**Files:**
- Modify: `apps/miniapp/src/pages/capture/index.tsx`
- Modify: `apps/miniapp/src/pages/analysis/index.tsx`
- Modify: `apps/miniapp/src/pages/report/index.tsx`
- Modify: `apps/miniapp/src/services/storage.ts`
- Delete: `apps/miniapp/src/services/task-utils.ts`
- Test: `packages/core/tests/assessment-report.test.mjs`

**Interfaces:**
- Consumes: Task 10 API/types/view helpers和基础计划 `useOperationPolling`。
- Produces: capture 以三槽位提交、analysis 只轮询 Operation、report 只读 Report 内嵌照片与 Finding；不发起方案生成。

- [ ] **Step 1: 扩充纯状态测试**

```js
test('assessment terminal navigation uses operation result report id', () => {
  assert.deepEqual(assessmentDestination({
    status:'succeeded', result_type:'report', result_id:REPORT,
  }), { kind:'report', url:`/pages/report/index?id=${REPORT}` })
})

test('assessment failure exposes exactly one action', () => {
  assert.deepEqual(assessmentDestination({
    status:'failed', error_code:'photo_content_rejected', retryable:false,
  }), { kind:'retake', url:'/pages/capture/index' })
})

test('finding projection keeps observation separate from recommendation', () => {
  const row = findingView(validFinding)
  assert.equal(row.observation, validFinding.visible_observation)
  assert.equal(row.recommendation, validFinding.recommendation)
  assert.equal('confidence' in row, false)
})
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `pnpm --filter @zsm/core test`

Expected: FAIL，`assessmentDestination` 和 `findingView` 未定义。

- [ ] **Step 3: 完成最小接线，不重设计页面**

`capture/index.tsx` 改为：

```ts
const assessmentKeyRef = useRef('')
if (!assessmentKeyRef.current) {
  assessmentKeyRef.current = createIdempotencyKey('assessment')
}
const accepted = await api.createAssessment({
  photos: {
    face_asset_id: slots.face!.asset.id,
    side_asset_id: slots.side!.asset.id,
    body_asset_id: slots.body!.asset.id,
  },
}, { idempotencyKey: assessmentKeyRef.current })
Taro.reLaunch({ url: `/pages/analysis/index?operation_id=${accepted.operation.id}` })
```

任一槽位被替换时先执行 `assessmentKeyRef.current = ''`；网络失败且三槽位未改变时保留该 key，确保重试命中基础幂等中间件。

`analysis/index.tsx` 只调用基础计划的 `useOperationPolling(operationID)`；成功时使用 `operation.result_id` 跳报告，失败时按 `error_code` 映射到 core 文案和唯一动作。删除本地 `activeTaskAnalysis`、客户端 6 分钟猜测超时和对 Analysis 行的轮询。

`report/index.tsx` 只执行：

```ts
const report = id ? await api.getReport(id) : await api.getCurrentReport()
const photos = reportPhotoMap(report)
const findings = report.findings.filter((item) => item.source_photo.role === activePhoto)
```

页面分别渲染 `visible_observation` 和 `recommendation`，锚点矩形用中心点 `x + w/2, y + h/2` 传给现有 `PhotoAnnotationLayer`。照片 URL 为空时在该槽显示“照片暂时无法显示”与“返回”动作；不得用 `current_image_url`、内置图或上一张缓存图兜底。Demo 只按 `source_kind === 'demo_example'` 显示“效果示例”。

报告页底部按钮只导航到既有场景入口：

```ts
Taro.navigateTo({ url: `/pages/scene/index?report_id=${encodeURIComponent(report.id)}` })
```

它不得在本计划中调用 `POST /v1/plan-sets`、旧 `upsertPlans` 或任何渲染端点。方案入口的数据消费由后续 Planning 计划负责。

- [ ] **Step 4: 删除旧本地业务引用并验证小程序**

Run: `rg 'reportId|taskId|activeTaskAnalysis|/v1/analyses|createAnalysis|getAnalysis|upsertPlans' apps/miniapp/src/pages/{capture,analysis,report} apps/miniapp/src/services/storage.ts`

Expected: 无匹配。

Run: `pnpm --filter @zsm/core test && pnpm --filter @zsm/miniapp typecheck && node apps/miniapp/scripts/check.mjs`

Expected: PASS；没有裸 `px`、没有 `{count && <View/>}`、没有索引 key，三页不读取旧 Analysis/Task。

- [ ] **Step 5: 提交小程序接线**

```bash
git add apps/miniapp/src/pages/capture/index.tsx apps/miniapp/src/pages/analysis/index.tsx apps/miniapp/src/pages/report/index.tsx apps/miniapp/src/services/storage.ts packages/core/tests/assessment-report.test.mjs
git rm apps/miniapp/src/services/task-utils.ts
git commit -m "feat(miniapp): connect assessment operations and reports"
```

### Task 12: Assessment/Report 金集 Fixture 与离线 Gate

**Files:**
- Create: `apps/server/eval/assessment/schema.go`
- Create: `apps/server/eval/assessment/runner.go`
- Create: `apps/server/eval/assessment/runner_test.go`
- Create: `apps/server/eval/assessment/testdata/manifest.json`
- Create: `apps/server/eval/assessment/testdata/cases/pass-evidence.json`
- Create: `apps/server/eval/assessment/testdata/cases/reject-sensitive.json`
- Create: `apps/server/eval/assessment/testdata/cases/retry-insufficient-evidence.json`
- Deferred to Peripherals/Cutover: `apps/server/cmd/eval/main.go`

**Interfaces:**
- Consumes: Task 1 `ValidateReportDraft` 与 Task 7 evidence policy。
- Produces: `assessment.Run(ctx, manifest, release) (Summary, error)`；Peripherals/Cutover 将该函数接入统一 `cmd/eval assessment` 子命令。

- [ ] **Step 1: 写 split、规模、证据支持率和红线测试**

```go
func TestValidateManifestPreventsIdentityLeakageAcrossSplits(t *testing.T) {
	m := loadFixture(t, "testdata/manifest.json")
	m.Cases[1].IdentityID = m.Cases[0].IdentityID
	m.Cases[1].Split = "validation"
	require.ErrorContains(t, ValidateManifest(m, false), "identity appears in multiple splits")
}

func TestReleaseThresholds(t *testing.T) {
	summary := Summary{
		AuthorizedIdentities:60, Photos:180, Reports:120,
		EvidenceLinked:360, EvidenceTotal:360,
		EvidenceSupported:343, SensitiveViolations:0,
		RoleChecks:180, RoleChecksCorrect:180,
	}
	require.NoError(t, CheckRelease(summary)) // 343/360 = 95.27%
	summary.EvidenceSupported = 341
	require.ErrorContains(t, CheckRelease(summary), "evidence support")
}

func TestFixtureRejectsSensitiveCopy(t *testing.T) {
	c := loadCase(t, "testdata/cases/reject-sensitive.json")
	require.ErrorIs(t, EvaluateCase(c).Err, assessment.ErrCopyPolicy)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd apps/server && go test ./eval/assessment -v`

Expected: FAIL，runner、manifest schema 和 fixture 不存在。

- [ ] **Step 3: 实现金集格式和门禁**

```json
{
  "schema_version": "assessment-golden.v1",
  "cases": [
    {
      "id": "pass-evidence",
      "identity_id": "synthetic-001",
      "authorized": true,
      "split": "development",
      "input": {
        "face": "fixture://face",
        "side": "fixture://side",
        "body": "fixture://body"
      },
      "expected": {
        "photo_roles_valid": true,
        "identity_consistent": true,
        "allowed_observations": ["上衣肩线低于自然肩点"],
        "forbidden_observations": ["身材评分"],
        "minimum_supported_findings": 3
      }
    }
  ]
}
```

checked-in manifest 只使用程序生成的合成图和去标识化 Provider JSON，不提交真人照片、签名 URL 或 embedding。`--release` 从私有对象存储挂载的同 schema manifest 读取 60 个明确授权身份、180 张三视图、120 个报告样本，并强制 development/validation/release 为 60%/20%/20%，同一 identity 不跨分区。

CLI 输出固定 JSON：

```json
{"role_accuracy":1,"evidence_link_rate":1,"evidence_support_rate":0.9527,"sensitive_violation_count":0,"passed":true}
```

退出码：通过为 0；schema、授权、分区、规模或阈值不通过为 1。外部模型不加入每次 PR CI；Staging/夜间任务运行 `--release`，只有锁定 release split 达标才允许发布对应路由配置。

- [ ] **Step 4: 运行 fixture 和 release policy tests**

Run: `cd apps/server && go test ./eval/assessment -v`

Expected: PASS；普通 fixture summary 为 `passed:true`，release-policy 用例明确断言 checked-in 合成 fixture 未达到 `60 identities / 180 photos / 120 reports`。

- [ ] **Step 5: 提交评测工具和合成 fixture**

```bash
git add apps/server/eval/assessment
git commit -m "test(server): add assessment golden evaluation"
```

### Task 13: 端到端服务端回归与范围清理

**Files:**
- Create: `apps/server/internal/service/assessment/e2e_test.go`
- Modify: `apps/server/scripts/e2e.sh`
- Modify: `docs/superpowers/plans/2026-09-12-assessment-report.md`（执行时只勾选已完成步骤，不改变设计）

**Interfaces:**
- Consumes: Tasks 1–12 的全部接口。
- Produces: 三槽位 → Operation → Gate → Report 的回归证据；仓库内无旧 Assessment/Report 双路径。

- [ ] **Step 1: 写完整链路测试**

```go
func TestAssessmentToPublishedReport(t *testing.T) {
	env := newAssessmentE2E(t)
	assets := env.UploadThreeValidPhotos(t)
	accepted := env.PostAssessment(t, assets, "e2e-assessment-1")
	require.Equal(t, "accepted", accepted.Operation.Status)

	env.RunTask(t, accepted.Operation.ID)
	op := env.GetOperation(t, accepted.Operation.ID)
	require.Equal(t, "succeeded", op.Status)
	require.Equal(t, "report", op.ResultType)

	report := env.GetReport(t, op.ResultID)
	require.Len(t, report.Photos, 3)
	require.GreaterOrEqual(t, len(report.Findings), 3)
	for _, finding := range report.Findings {
		require.NotEmpty(t, finding.SourcePhoto.ItemID)
		require.NotEmpty(t, finding.VisibleObservation)
		require.NotEmpty(t, finding.Recommendation)
	}
}

func TestRejectedPhotosNeverCreateReadableReport(t *testing.T) {
	env := newAssessmentE2E(t)
	accepted := env.PostAssessment(t, env.UploadWrongRolePhotos(t), "e2e-assessment-2")
	env.RunTask(t, accepted.Operation.ID)
	op := env.GetOperation(t, accepted.Operation.ID)
	require.Equal(t, "failed", op.Status)
	require.Equal(t, "photo_content_rejected", op.ErrorCode)
	require.Empty(t, op.ResultID)
	require.Equal(t, 0, env.ReportCount(t))
}
```

- [ ] **Step 2: 运行端到端测试并确认失败**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/service/assessment -run 'TestAssessmentToPublishedReport|TestRejectedPhotosNeverCreateReadableReport' -v`

Expected: 首次 FAIL 于尚未接入的 fake Provider/HTTP fixture；补齐测试装配后再进入下一步，不改变生产行为。

- [ ] **Step 3: 补齐 e2e fake，只替换 Provider Port**

E2E 使用真实 PostgreSQL、真实 Service/Repository/HTTP handler、内存 ObjectStorage 和确定性 Provider fake。fake 必须返回 Task 12 的 `pass-evidence.json`，不得绕过技术检查、Gate 或发布事务。

- [ ] **Step 4: 执行范围扫描与全量验证**

Run: `rg 'beauty_score|body_score|颜值分|身材分|severity|current_image_url|analysis_id|media_ids|/v1/analyses|provider_version|confidence' apps/server/internal/{domain,httpapi,service/assessment} contracts/openapi.yaml packages/core/src apps/miniapp/src/pages/{capture,analysis,report}`

Expected: 只允许以下内部匹配：`report_findings.confidence` 的持久化映射、Provider DTO confidence、copy-policy 测试中的禁词；HTTP DTO、OpenAPI、core 类型和页面无 confidence/provider_version/旧字段。

Run: `pnpm typecheck && pnpm lint && make design-build`

Expected: PASS。

Run: `node apps/miniapp/scripts/check.mjs`

Expected: PASS。

Run: `make server-vet && make server-test`

Expected: PASS。

Run: `node contracts/scripts/check-sync.mjs`

Expected: PASS，输出“契约与 core 端点完全一致”。

- [ ] **Step 5: 提交最终回归边界**

```bash
git add apps/server/internal/service/assessment/e2e_test.go apps/server/scripts/e2e.sh docs/superpowers/plans/2026-09-12-assessment-report.md
git commit -m "test: verify assessment report quality chain"
```

## 执行完成判定

以下条件必须同时满足，才能进入后续 Planning/RenderSpec 计划：

1. 相同 PhotoSet 内容复用 PhotoSet，相同分析 input hash 复用 AnalysisRun/Operation/Task。
2. 三槽位缺失、重复、错角色、跨用户、非 ready、混合 Demo/用户来源全部有测试且失败关闭。
3. JPEG/PNG 魔数、MIME、尺寸、像素、完整解码检查在 AI 调用前完成。
4. 内容检查和同人检查在 appearance analysis 前完成；同人失败不保存 embedding、不向用户输出概率。
5. 每个已发布 Finding 都有 source PhotoSetItem、矩形 anchor、visible observation 和 recommendation。
6. 不支持的 Finding 被删除；少于 3 条只补生成一次；第二次仍不足不发布 Report。
7. Report/Finding/QualityEvaluation 更新或删除被数据库拒绝。
8. Operation 成功只指向已完整提交的 Report；失败 Operation 没有 result ID。
9. `/v1`、core 和小程序不存在旧 Analysis/Task 双路径或本地业务 ID fallback。
10. 报告页不发起方案或图片渲染请求，不显示 confidence，不用示例图填补用户图空缺。
11. checked-in 合成 fixture 可在 PR 中稳定运行，私有授权金集的 release 模式强制 60/180/120、身份不跨 split、证据支持率 ≥95% 和敏感违规 0。
12. 仓库规定的全部验证命令通过。

计划执行完后，下一份计划从已发布不可变 Report Reader 开始构建 PlanSet；不得回写或扩展本计划的 Report/Finding 语义。
