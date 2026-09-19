# Quality Core Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用一个全新 PostgreSQL baseline、按域拆分的 Go 类型和窄 Port，建立可追溯且可并发安全的 Media/UploadIntent、公开 Operation、内部 Task、Provider Invocation、幂等和基础 Billing reservation，并让 API 与 Worker 作为两个独立进程运行。

**Architecture:** 保持一个 Go 模块、API/Worker 两个进程、一个 PostgreSQL 和一个私有 COS；`httpapi → service → repository/provider/storage`，业务服务只依赖自己 `ports.go` 中的最小接口。公开 Operation 与内部 Task 分离；Worker 通过 PostgreSQL `FOR UPDATE SKIP LOCKED`、lease token、heartbeat 和业务 generation CAS 执行任务，外部调用期间不持有数据库事务。

**Tech Stack:** Go 1.24.0（toolchain 1.24.5）、PostgreSQL 16、pgx/v5、`net/http`、腾讯云 COS Go SDK、OpenAPI 3.1、Docker Compose。

## Global Constraints

- `appearance-coach-prototype/` 只读；本计划不得修改该目录。
- 根级 `package.json`、`tsconfig.base.json`、`Makefile`、`docker-compose.yml` 已冻结；本计划不修改根文件，唯一 root task 位于最终 Peripherals/Cutover 计划。
- Go module 固定为 `github.com/zhanshimian/server`；不得改根包名或引入第二个 Go module。
- 依赖方向固定为 `httpapi → service → repository / provider / storage`；handler 不得直接访问 PostgreSQL。
- Repository 每次用户资源读写必须同时校验 `resource_id + user_id`；越权与不存在统一返回 404。
- 所有用户资源使用 `id uuid primary key`、`user_id uuid not null`、`created_at timestamptz not null`、`unique(user_id, id)`；跨资源关系使用 `(user_id, parent_id)` 复合外键。
- 数据库只保存 COS object key，不保存签名 URL；对象 key 不可变且全局唯一。
- 用户上传只接受 JPEG/PNG；Provider 输出发布给小程序前必须是 JPEG；不得发布 WebP。
- Demo 只经 `/v1/media/demo` 与 Demo Provider 进入；生产不得自动切 Demo。
- `provider_version` 以 `demo` 开头的结果必须按“效果示例”处理；不得用用户图、内置图或 Demo 冒充生成结果。
- 客户端只读取公开 Operation；不得返回 Task payload、Provider 厂商、模型名、临时 Provider URL或内部质量分数。
- Task 类型不使用数据库 CHECK 枚举；Handler Registry 是任务类型、超时、并发和重试预算的唯一注册源。
- Claim 必须使用 `FOR UPDATE SKIP LOCKED`；Worker 必须 heartbeat；成功提交必须同时校验 lease token 和目标 generation。
- 外部 Provider 调用不得持有数据库事务；错误必须归类为 `transient | throttled | permanent | quality_rejected | superseded`，不得搜索错误字符串。
- AI 业务调用只经能力路由配置；业务代码不得出现厂商名或模型名。
- 所有创建请求支持 `Idempotency-Key`；相同用户和 key 的不同请求返回冲突，相同请求只产生一次副作用。
- 付费 Operation 以 Operation ID 预占；结算引用最终业务结果，退款引用 Operation，不绑定可重试 Task；内部质量补生成不得重复扣费。
- API 生产环境不得内嵌 Worker；`cmd/worker` 独立启动和扩缩容。
- 不引入微服务、Redis、Kafka、消息中间件、工作流引擎、Saga、事件溯源、兼容数据库、兼容 DTO、旧数据回填或双写。
- 不建立泛型 BaseRepository、全能 Service、全能 Repository、ProviderOptions 参数袋或通用事件总线。
- 不持久化人脸 embedding、API key、原始图片字节、data URL、完整用户照片 URL、未脱敏手机号或 OpenID。
- 不存 beauty_score、body_score、年龄分、排名或百分位；不输出医学、健康、族裔、性格或社会身份推断。
- 迁移只向前、embed 并按文件名顺序执行；本重建直接删除旧数据库/Volume，不写旧数据迁移。
- 本计划执行期间，每个 Task 只提交自己的文件；文档中的 Commit 是实施时建议，本次编写计划不执行提交。

---

## Contract Ownership Override

- 执行本计划前，先完成总索引的 Rule/Contract Freeze 和 Miniapp Task 1。
- `contracts/**`、OpenAPI codegen 和 `packages/core/src/api/**` 由该冻结阶段独占；Foundation 只消费已生成的 Operation/Media 契约并编写 HTTP contract tests。
- 下文 Tasks 中任何 `Modify: contracts/openapi.yaml` 或提交该文件的步骤均改为 `Verify`，不得在 Foundation 内改变传输 DTO。

## Exact File Structure

以下是本计划结束时与质量核心基础层直接相关的精确文件结构。未列出的登录、短信、支付 Adapter 和外围业务文件保持原位，后续计划再通过窄 Reader 接入。

```text
contracts/
  openapi.yaml                                  # UploadIntent、MediaAsset、Operation 与错误契约
  scripts/check-sync.mjs                        # 校验新公开路径与 Go 路由一致
apps/server/
  Dockerfile                                    # 同一镜像构建 api 与 worker 两个二进制
  cmd/
    api/main.go                                 # 只启动 HTTP API
    worker/main.go                              # 只启动 task runner
  internal/
    bootstrap/
      api.go                                    # API 依赖装配
      worker.go                                 # Worker Registry 与 Runner 装配
      api_test.go
      worker_test.go
    config/
      config.go                                 # lease、heartbeat、worker ID、并发配置
      config_test.go
    database/
      database.go                               # embed baseline 并保持文件名顺序执行
      database_test.go
      migrations/
        001_baseline.sql                        # 全新无兼容 baseline
    domain/
      identity.go                               # User、Identity、Session
      profile.go                                # UserProfile 与 version
      media.go                                  # MediaAsset、UploadIntent、上传对象元数据
      quality.go                                # 跨报告/方案/渲染共用 QualityEvaluation
      operation.go                              # 公开 Operation 状态
      task.go                                   # 内部 Task、Lease、分类错误、结果
      invocation.go                             # Provider 调用账本类型
      billing.go                                # Wallet、Reservation、Ledger 类型
      idempotency.go                            # HTTP 幂等记录类型
    httpapi/
      httpapi.go                                # 路由、统一 envelope、retryable 错误字段
      idempotency.go                            # Idempotency-Key 中间件
      idempotency_test.go
      media.go                                  # 上传意图创建/完成
      media_test.go
      operations.go                             # 单个/批量 Operation 查询
      operations_test.go
    service/
      media/
        service.go
        ports.go
        validation.go
        service_test.go
      operation/
        service.go
        ports.go
        service_test.go
      billing/
        service.go
        ports.go
        service_test.go
      taskrunner/
        runner.go
        registry.go
        ports.go
        runner_test.go
        registry_test.go
    repository/
      errors.go
      postgres/
        store.go
        media.go
        media_test.go
        operations.go
        operations_test.go
        tasks.go
        tasks_test.go
        provider_invocations.go
        provider_invocations_test.go
        idempotency.go
        idempotency_test.go
        billing.go
        billing_test.go
    provider/
      ai/
        invocations.go                          # 调用前后记账装饰器
        invocations_test.go
    storage/
      storage.go                                # 基础对象接口
      cos.go                                    # Presign PUT、HEAD 与签名 GET
      cos_test.go
      local.go
    testutil/
      postgres.go                               # 每测试独立临时数据库
```

删除以下旧迁移，不保留兼容或回填：

```text
apps/server/internal/database/migrations/001_init.sql
apps/server/internal/database/migrations/002_advisor_tools.sql
apps/server/internal/database/migrations/003_hair_previews.sql
apps/server/internal/database/migrations/004_scene_plans.sql
apps/server/internal/database/migrations/005_daily_share_wardrobe_advisor.sql
apps/server/internal/database/migrations/006_brand_rename.sql
apps/server/internal/database/migrations/007_report_finding_detail.sql
apps/server/internal/database/migrations/008_plan_look_generation.sql
apps/server/internal/database/migrations/009_feedback_media.sql
apps/server/internal/database/migrations/010_report_finding_photo.sql
apps/server/internal/database/migrations/011_today_plan_generation.sql
apps/server/internal/database/migrations/012_multi_channel_identity.sql
apps/server/internal/database/migrations/013_user_profiles.sql
apps/server/internal/database/migrations/014_sms_codes.sql
apps/server/internal/database/migrations/015_unified_tasks.sql
apps/server/internal/database/migrations/016_brand_rename_up.sql
apps/server/internal/database/migrations/017_plan_group_tasks.sql
apps/server/internal/database/migrations/018_brand_rename_uplook.sql
apps/server/internal/database/migrations/019_billing.sql
apps/server/internal/database/migrations/020_user_avatar.sql
```

### Cross-Task Interface Ledger

后续 Task 必须原样消费这些签名，不得另造同义接口。

```go
// service/media/ports.go
type Repository interface {
	CreateUploadIntent(context.Context, domain.CreateUploadIntent) (domain.UploadIntent, error)
	GetUploadIntent(context.Context, string, string) (domain.UploadIntent, error)
	CompleteUploadIntent(context.Context, domain.CompleteUploadIntent) (domain.MediaAsset, bool, error)
}
type ObjectStore interface {
	PresignUpload(context.Context, domain.UploadIntent, time.Duration) (domain.UploadGrant, error)
	HeadObject(context.Context, string) (domain.ObjectMetadata, error)
}

// service/operation/ports.go
type Reader interface {
	GetOperation(context.Context, string, string) (domain.Operation, error)
	GetOperations(context.Context, string, []string) ([]domain.Operation, error)
}

// httpapi/idempotency.go
type IdempotencyStore interface {
	BeginIdempotency(context.Context, domain.BeginIdempotency) (domain.IdempotencyRecord, domain.IdempotencyBeginOutcome, error)
	CompleteIdempotency(context.Context, string, string, int, json.RawMessage) error
	AbortIdempotency(context.Context, string, string) error
}

// service/taskrunner/ports.go
type TaskStore interface {
	Claim(context.Context, string, time.Duration, []domain.TaskType) (domain.TaskLease, bool, error)
	Heartbeat(context.Context, domain.TaskLease, time.Duration) (bool, error)
	Fail(context.Context, domain.TaskLease, domain.TaskFailure, time.Time) (bool, error)
}
type Handler interface {
	Type() domain.TaskType
	Execute(context.Context, domain.TaskLease) (domain.TaskResult, error)
	Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
}

// provider/ai/invocations.go
type InvocationStore interface {
	StartInvocation(context.Context, domain.StartInvocation) (domain.ProviderInvocation, error)
	FinishInvocation(context.Context, domain.FinishInvocation) error
}

// service/billing/ports.go
type Repository interface {
	Reserve(context.Context, domain.ReserveBilling) (domain.BillingReservation, bool, error)
	Settle(context.Context, domain.SettleBilling) (domain.BillingReservation, bool, error)
	Refund(context.Context, domain.RefundBilling) (domain.BillingReservation, bool, error)
}
```

## Task 1: Replace Historical Migrations with the New Baseline

**Files:**
- Delete: the 20 exact files listed under “删除以下旧迁移”
- Create: `apps/server/internal/database/migrations/001_baseline.sql`
- Create: `apps/server/internal/testutil/postgres.go`
- Create: `apps/server/internal/database/database_test.go`
- Modify: `apps/server/internal/database/database.go`

**Interfaces:**
- Consumes: `database.Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)` and `database.Migrate(ctx context.Context, pool *pgxpool.Pool) error`.
- Produces: `testutil.NewPostgres(t testing.TB) *pgxpool.Pool`; one fresh schema containing every table and constraint listed below.

- [ ] **Step 1: Add a failing fresh-database schema test**

```go
func TestBaselineCreatesQualityCoreSchema(t *testing.T) {
	pool := testutil.NewPostgres(t)
	expected := []string{
		"users", "user_identities", "user_sessions", "user_profiles",
		"media_assets", "upload_intents", "photo_sets", "photo_set_items",
		"operations", "tasks", "provider_invocations", "idempotency_keys",
		"analysis_runs", "reports", "report_findings",
		"plan_sets", "plan_variants", "plan_steps", "plan_step_groundings",
		"render_specs", "render_heads", "render_runs", "render_candidates",
		"quality_evaluations", "render_publications",
		"plan_selections", "executions", "execution_steps", "execution_events",
		"generation_feedback", "execution_feedback", "object_gc_jobs",
		"billing_wallets", "billing_reservations", "billing_ledger",
	}
	for _, table := range expected {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists)
		require.NoError(t, err)
		require.Truef(t, exists, "missing table %s", table)
	}
}

func TestBaselineRejectsCrossUserMediaAttachment(t *testing.T) {
	pool := testutil.NewPostgres(t)
	userA := insertUser(t, pool)
	userB := insertUser(t, pool)
	asset := insertMedia(t, pool, userA, "user_upload", "face")
	photoSet := insertPhotoSet(t, pool, userB)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO photo_set_items(user_id,photo_set_id,role,media_asset_id)
		VALUES($1,$2,'face',$3)`, userB, photoSet, asset)
	require.Error(t, err)
	require.Equal(t, "23503", err.(*pgconn.PgError).Code)
}
```

- [ ] **Step 2: Run the schema tests and verify the old migration set fails the new contract**

Run:

```bash
docker compose up -d postgres
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/database -run 'TestBaseline' -v
```

Expected: FAIL; `TestBaselineCreatesQualityCoreSchema` reports `missing table upload_intents` or `missing table operations`.

- [ ] **Step 3: Add the isolated PostgreSQL test harness**

Implement `testutil.NewPostgres` to parse `TEST_DATABASE_URL`, create a database named `zsm_test_<12 hex chars>`, call `database.Open` and `database.Migrate`, and register cleanup that closes the pool, terminates remaining connections, and drops that database. Quote database identifiers with `pgx.Identifier{dbName}.Sanitize()`; fail immediately when `TEST_DATABASE_URL` is absent instead of silently skipping integration tests.

```go
func NewPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	require.NotEmpty(t, adminURL, "TEST_DATABASE_URL is required")
	admin, err := pgxpool.New(context.Background(), adminURL)
	require.NoError(t, err)
	dbName := "zsm_test_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	_, err = admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
	require.NoError(t, err)
	testURL := withDatabase(t, adminURL, dbName)
	pool, err := database.Open(context.Background(), testURL)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background(), pool))
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(),
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1`, dbName)
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
	})
	return pool
}
```

- [ ] **Step 4: Replace all historical SQL with one baseline**

`001_baseline.sql` must define every table in Step 1. Apply these exact cross-cutting rules:

- every user-owned entity has `UNIQUE(user_id,id)`;
- every parent reference is `(user_id,parent_id)` and targets a matching composite unique key;
- all enum-like stable business states use CHECK constraints except `tasks.type`;
- `media_assets.object_key` is globally unique; Provider output requires `provider_invocation_id`; Demo requires `display_kind='effect_example'`; quarantined candidates cannot be marked published;
- `upload_intents` stores expected MIME, bytes and SHA-256, expires in 15 minutes, and has one optional completed asset;
- `photo_set_items` has unique `(photo_set_id,role)` and `(photo_set_id,media_asset_id)`;
- immutable tables have no `updated_at`;
- mutable `user_profiles`, `render_heads`, `executions`, `operations`, `tasks`, `upload_intents`, `billing_wallets` and `billing_reservations` carry the specified `version` or state-transition columns;
- `tasks` includes `lease_token uuid`, `lease_owner`, `lease_expires_at`, `heartbeat_at`, `cancel_requested_at`, `dedupe_key`, `attempt`, `max_attempts`, `subject_generation`, typed error columns and `UNIQUE(user_id,dedupe_key)`;
- `provider_invocations` stores routing metadata and cost but no secrets or input bytes;
- `idempotency_keys` has `UNIQUE(user_id,key)`, request fingerprint, `in_progress|completed`, response status/body and expiry;
- billing references `operation_id`; reservation is unique per Operation and ledger is unique by `(user_id,reason,reference_type,reference_id)`;
- add claim index `(status,available_at,priority DESC,created_at)` for `queued|retry_wait`, lease-expiry index for `leased`, Operation user/status index, invocation trace indexes and 24-hour upload cleanup index.

Critical DDL shape:

```sql
CREATE TABLE tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  operation_id uuid NOT NULL,
  type text NOT NULL,
  subject_type text NOT NULL,
  subject_id uuid NOT NULL,
  subject_generation bigint NOT NULL DEFAULT 0,
  payload_version int NOT NULL CHECK (payload_version > 0),
  payload jsonb NOT NULL,
  dedupe_key text NOT NULL,
  status text NOT NULL CHECK (status IN
    ('queued','leased','retry_wait','succeeded','failed','cancelled','superseded')),
  priority int NOT NULL DEFAULT 0,
  attempt int NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  max_attempts int NOT NULL CHECK (max_attempts > 0),
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_token uuid,
  lease_owner text,
  lease_expires_at timestamptz,
  heartbeat_at timestamptz,
  cancel_requested_at timestamptz,
  progress_bps int NOT NULL DEFAULT 0 CHECK (progress_bps BETWEEN 0 AND 10000),
  stage_code text NOT NULL,
  error_class text CHECK (error_class IN
    ('transient','throttled','permanent','quality_rejected','superseded')),
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE(user_id,id),
  UNIQUE(user_id,dedupe_key),
  FOREIGN KEY(user_id,operation_id) REFERENCES operations(user_id,id) ON DELETE CASCADE,
  CHECK (
    (status='leased' AND lease_token IS NOT NULL AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
    OR status<>'leased'
  )
);
```

- [ ] **Step 5: Verify migration idempotence and all schema constraints**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/database -run 'TestBaseline' -count=1 -v
```

Expected: PASS; both schema inventory and cross-user foreign-key tests pass, and calling `database.Migrate` twice leaves exactly one `schema_migrations` row for `001_baseline.sql`.

**Commit suggestion:**

```bash
git add apps/server/internal/database apps/server/internal/testutil
git commit -m "feat(server): replace schema with quality core baseline"
```

## Task 2: Split Foundation Domain Types and Repository Errors

**Files:**
- Create: `apps/server/internal/domain/identity.go`
- Create: `apps/server/internal/domain/profile.go`
- Create: `apps/server/internal/domain/media.go`
- Create: `apps/server/internal/domain/quality.go`
- Create: `apps/server/internal/domain/operation.go`
- Create: `apps/server/internal/domain/task.go`
- Create: `apps/server/internal/domain/task_test.go`
- Create: `apps/server/internal/domain/invocation.go`
- Modify: `apps/server/internal/domain/billing.go`
- Create: `apps/server/internal/domain/idempotency.go`
- Create: `apps/server/internal/repository/errors.go`
- Modify: `apps/server/internal/domain/domain.go`
- Modify: `apps/server/internal/repository/repository.go`

**Interfaces:**
- Consumes: baseline column names and enums from Task 1.
- Produces: typed constants and command/result structs used verbatim by Tasks 3–9; `repository.ErrNotFound`, `repository.ErrConflict`, `repository.ErrLeaseLost`.

- [ ] **Step 1: Add compile-time domain contract tests**

Create `apps/server/internal/domain/task_test.go`. The contract test must assert terminal states and typed classifications:

```go
func TestTaskStateClassification(t *testing.T) {
	require.False(t, domain.TaskStatus("queued").Terminal())
	require.False(t, domain.TaskStatus("leased").Terminal())
	require.True(t, domain.TaskStatus("succeeded").Terminal())
	require.True(t, domain.TaskStatus("failed").Terminal())
	require.True(t, domain.TaskStatus("cancelled").Terminal())
	require.True(t, domain.TaskStatus("superseded").Terminal())
	require.True(t, domain.ErrorTransient.Retryable())
	require.True(t, domain.ErrorThrottled.Retryable())
	require.False(t, domain.ErrorPermanent.Retryable())
	require.False(t, domain.ErrorQualityRejected.Retryable())
	require.False(t, domain.ErrorSuperseded.Retryable())
}
```

- [ ] **Step 2: Run the domain package and observe undefined new types**

Run:

```bash
cd apps/server && go test ./internal/domain -run 'TestTaskStateClassification' -v
```

Expected: FAIL to compile with `undefined: domain.TaskStatus` or `undefined: domain.ErrorTransient`.

- [ ] **Step 3: Move foundation types out of the type warehouse**

Define IDs as strings at transport/service boundaries and preserve `time.Time` timestamps. Use these exact key types:

```go
type MediaOrigin string
type MediaPurpose string
type MediaState string
type DisplayKind string
type OperationKind string
type OperationStatus string
type TaskType string
type TaskStatus string
type ErrorClass string
type InvocationStatus string
type BillingReservationStatus string

type TaskLease struct {
	Task
	LeaseToken string
	LeaseOwner string
	LeaseExpiresAt time.Time
}

type TaskResult struct {
	Disposition TaskDisposition
	ResultType string
	ResultID string
	Failure *TaskFailure
}

type TaskDisposition string
const (
	TaskPublish TaskDisposition = "publish"
	TaskEnqueueNext TaskDisposition = "enqueue_next"
	TaskDomainFail TaskDisposition = "domain_failed"
)

type CommitOutcome string

const (
	CommitApplied CommitOutcome = "applied"
	CommitSuperseded CommitOutcome = "superseded"
)
```

Foundation 不创建 `assessment.go`、`planning.go`、`rendering.go`、`execution.go` 或 `feedback.go`；这些文件由各自领域计划独占。`quality.go` 只定义跨报告、方案和渲染共用的 `QualityEvaluation`、`QualityDecision` 与版本化策略引用，后续计划只能引用，不能重复声明。把已迁出的 foundation 类型从 `domain.go` 删除；不得保留别名或旧字段桥接。`repository.Repository` 在旧业务删除计划执行前不增加任何新方法；本 Task 只把公共错误声明移动到 `repository/errors.go`。

- [ ] **Step 4: Verify package-level type consistency**

Run:

```bash
cd apps/server && go test ./internal/domain ./internal/repository -count=1
```

Expected: PASS;不存在重复类型声明，`TaskStatus.Terminal` 和 `ErrorClass.Retryable` 全部断言通过。

**Commit suggestion:**

```bash
git add apps/server/internal/domain apps/server/internal/repository/errors.go apps/server/internal/repository/repository.go
git commit -m "refactor(server): split quality core domain types"
```

## Task 3: Implement MediaAsset and UploadIntent as a Vertical Slice

**Files:**
- Create: `apps/server/internal/service/media/ports.go`
- Create: `apps/server/internal/service/media/validation.go`
- Create: `apps/server/internal/service/media/service.go`
- Create: `apps/server/internal/service/media/service_test.go`
- Create: `apps/server/internal/repository/postgres/store.go`
- Modify: `apps/server/internal/repository/postgres/postgres.go`
- Create: `apps/server/internal/repository/postgres/media.go`
- Create: `apps/server/internal/repository/postgres/media_test.go`
- Modify: `apps/server/internal/storage/storage.go`
- Modify: `apps/server/internal/storage/cos.go`
- Modify: `apps/server/internal/storage/cos_test.go`
- Modify: `apps/server/internal/storage/local.go`
- Modify: `apps/server/internal/httpapi/media.go`
- Create: `apps/server/internal/httpapi/media_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`
- Modify: `contracts/scripts/check-sync.mjs`

**Interfaces:**
- Consumes: `domain.UploadIntent`, `domain.UploadGrant`, `domain.ObjectMetadata`, `domain.MediaAsset`; Task 2 repository errors.
- Produces: `media.Repository` and `media.ObjectStore` exactly as declared in the interface ledger; `POST /v1/media/upload-intents`; `POST /v1/media/upload-intents/{id}/complete`.

- [ ] **Step 1: Write service tests for intent creation and verified completion**

```go
func TestCompleteUploadRejectsHeadMismatch(t *testing.T) {
	repo := newMediaRepoFake()
	store := &objectStoreFake{head: domain.ObjectMetadata{
		ObjectKey: "users/u1/uploads/i1", MIMEType: "image/png",
		ByteSize: 21, SHA256: strings.Repeat("a", 64),
	}}
	svc := media.New(repo, store, 10<<20, 15*time.Minute)
	intent, err := svc.CreateUploadIntent(context.Background(), "u1", media.CreateIntentInput{
		Purpose: domain.MediaPurposeFace,
		MIMEType: "image/jpeg",
		ByteSize: 20,
		SHA256: strings.Repeat("b", 64),
	})
	require.NoError(t, err)
	_, err = svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	require.ErrorIs(t, err, media.ErrUploadMetadataMismatch)
	require.Zero(t, repo.completeCalls)
}

func TestCompleteUploadIsIdempotent(t *testing.T) {
	repo := newMediaRepoFake()
	store := matchingObjectStore()
	svc := media.New(repo, store, 10<<20, 15*time.Minute)
	intent := createIntent(t, svc, "u1")
	first, err := svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	require.NoError(t, err)
	second, err := svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 1, repo.insertAssetCalls)
}
```

- [ ] **Step 2: Run focused tests and verify the vertical slice is absent**

Run:

```bash
cd apps/server && go test ./internal/service/media ./internal/httpapi -run 'TestCompleteUpload|TestUploadIntent' -v
```

Expected: FAIL to compile because `internal/service/media` and the new handlers do not exist.

- [ ] **Step 3: Implement the minimal media service and Postgres adapter**

`CreateUploadIntent` validates purpose in `face|side|body|feedback|wardrobe`, MIME in `image/jpeg|image/png`, byte size `1..MaxUploadBytes`, and lowercase 64-character SHA-256. Generate object key `users/{userID}/uploads/{intentID}` server-side. `CompleteUploadIntent` performs `GetUploadIntent`, then `HeadObject` outside any DB transaction, compares object key/MIME/bytes/SHA, and finally calls repository completion.

Move only `Store`、`New`、`mapNotFound` and shared pgx error helpers from `postgres.go` into `store.go`; leave unrelated legacy adapter methods in `postgres.go` until their owning rebuild plans replace them. `postgres.CompleteUploadIntent` must lock the intent by `(user_id,id)`, return its existing Asset when already completed, reject expired/non-pending intents, insert exactly one `media_assets` row with `origin=user_upload`, `state=ready`, `display_kind=original`, then CAS the intent from `pending` to `completed` in one transaction.

```go
func (s *Service) CompleteUploadIntent(ctx context.Context, userID, intentID string) (domain.MediaAsset, error) {
	intent, err := s.repo.GetUploadIntent(ctx, userID, intentID)
	if err != nil {
		return domain.MediaAsset{}, err
	}
	meta, err := s.objects.HeadObject(ctx, intent.ObjectKey)
	if err != nil {
		return domain.MediaAsset{}, fmt.Errorf("head upload object: %w", err)
	}
	if err := validateObject(intent, meta); err != nil {
		return domain.MediaAsset{}, err
	}
	asset, _, err := s.repo.CompleteUploadIntent(ctx, domain.CompleteUploadIntent{
		UserID: userID, IntentID: intentID, Metadata: meta,
	})
	return asset, err
}
```

- [ ] **Step 4: Implement COS upload signing and HTTP/OpenAPI contracts**

`COS.PresignUpload` returns a 15-minute PUT URL and mandatory request headers `Content-Type`, `Content-Length`, `x-cos-meta-sha256`; `COS.HeadObject` returns object key, MIME, bytes and SHA metadata. `Local` returns `storage.ErrDirectUploadUnavailable`; production configuration already requires COS, and HTTP maps this development-only limitation to 503 without inventing a second upload protocol.

The create response is:

```json
{
  "data": {
    "id": "uuid",
    "purpose": "face",
    "status": "pending",
    "upload": {
      "method": "PUT",
      "url": "https://private-cos.example/signed",
      "headers": {
        "Content-Type": "image/jpeg",
        "x-cos-meta-sha256": "64-lowercase-hex"
      },
      "expires_at": "2026-09-12T15:15:00Z"
    }
  }
}
```

Completion returns `201 {"data": MediaAsset}` on first completion and `200` with the same Asset on replay. Remove the old multipart `POST /v1/media` route; keep `/v1/media/demo` only as an explicit Demo route for later provider wiring.

- [ ] **Step 5: Run service, adapter, HTTP and contract tests**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/service/media ./internal/repository/postgres ./internal/httpapi ./internal/storage -run 'Media|Upload|COS' -count=1
cd ../.. && node contracts/scripts/check-sync.mjs
```

Expected: PASS; metadata mismatch creates no Asset, a double complete returns one Asset ID, cross-user completion is not found, and contract sync recognizes both upload-intent endpoints.

**Commit suggestion:**

```bash
git add contracts apps/server/internal/domain/media.go apps/server/internal/service/media apps/server/internal/repository/postgres/media.go apps/server/internal/repository/postgres/media_test.go apps/server/internal/storage apps/server/internal/httpapi
git commit -m "feat(server): add verified upload intents and media assets"
```

## Task 4: Add Public Operations Without Exposing Internal Tasks

**Files:**
- Create: `apps/server/internal/service/operation/ports.go`
- Create: `apps/server/internal/service/operation/service.go`
- Create: `apps/server/internal/service/operation/service_test.go`
- Create: `apps/server/internal/repository/postgres/operations.go`
- Create: `apps/server/internal/repository/postgres/operations_test.go`
- Create: `apps/server/internal/httpapi/operations.go`
- Create: `apps/server/internal/httpapi/operations_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`
- Modify: `contracts/scripts/check-sync.mjs`
- Delete: `apps/server/internal/httpapi/tasks.go`

**Interfaces:**
- Consumes: `operation.Reader` from the interface ledger and `domain.Operation`.
- Produces: `operation.Service.Get(ctx,userID,id)`; `operation.Service.GetMany(ctx,userID,ids)`; `GET /v1/operations/{id}`; `GET /v1/operations?ids=`.

- [ ] **Step 1: Write HTTP privacy and ownership tests**

```go
func TestGetOperationHidesTaskInternals(t *testing.T) {
	op := domain.Operation{
		ID: "op1", UserID: "u1", Kind: domain.OperationAssessment,
		Status: domain.OperationRunning, ProgressBPS: 3500,
		StageCode: "photo.technical_check", PublicMessage: "正在检查照片",
	}
	api := newOperationAPI(t, op)
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations/op1", nil)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "payload")
	require.NotContains(t, response.Body.String(), "provider")
	require.NotContains(t, response.Body.String(), "lease")
	require.JSONEq(t, `{"data":{"id":"op1","kind":"assessment","status":"running","progress_bps":3500,"stage_code":"photo.technical_check","public_message":"正在检查照片","retryable":false}}`, response.Body.String())
}

func TestGetOperationFromAnotherUserReturnsNotFound(t *testing.T) {
	api := newOperationAPI(t, domain.Operation{ID: "op1", UserID: "u1"})
	response := authenticatedRequest(t, api, "u2", http.MethodGet, "/v1/operations/op1", nil)
	require.Equal(t, http.StatusNotFound, response.Code)
}
```

- [ ] **Step 2: Run focused tests and verify Operation routes are missing**

Run:

```bash
cd apps/server && go test ./internal/httpapi ./internal/service/operation -run 'TestGetOperation' -v
```

Expected: FAIL to compile because `service/operation` and Operation handlers are absent.

- [ ] **Step 3: Implement the Reader, service and user-scoped Postgres queries**

`GetOperations` accepts 1–20 UUIDs, rejects duplicates, queries with both `user_id` and IDs, and returns results in request order. If any requested ID is absent for that user, return `repository.ErrNotFound`; do not reveal which ID existed for another user.

Operation JSON includes only `id,kind,subject_type,subject_id,status,progress_bps,stage_code,public_message,error_code,trace_id,retryable,result_type,result_id,created_at,updated_at,finished_at`. Failed terminal Operations must include `trace_id`; it never includes `user_id`.

- [ ] **Step 4: Replace public Task routes in OpenAPI and HTTP routing**

Delete authenticated `GET /v1/tasks/{id}` and `GET /v1/tasks`. Register the two Operation routes. Define statuses exactly as `accepted|running|retrying|succeeded|failed|cancelled|superseded`; define kinds exactly as `assessment|plan_set|render|execution_feedback`. Batch responses preserve the requested ID order.

- [ ] **Step 5: Verify Operation contracts**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/service/operation ./internal/repository/postgres ./internal/httpapi -run 'Operation' -count=1
cd ../.. && node contracts/scripts/check-sync.mjs
```

Expected: PASS; no public `/v1/tasks` path remains, cross-user reads return 404, and Operation responses contain no internal fields.

**Commit suggestion:**

```bash
git add contracts apps/server/internal/service/operation apps/server/internal/repository/postgres/operations.go apps/server/internal/repository/postgres/operations_test.go apps/server/internal/httpapi
git commit -m "feat(server): expose public operations"
```

## Task 5: Enforce Request Idempotency at the HTTP Boundary

**Files:**
- Create: `apps/server/internal/httpapi/idempotency.go`
- Create: `apps/server/internal/httpapi/idempotency_test.go`
- Create: `apps/server/internal/repository/postgres/idempotency.go`
- Create: `apps/server/internal/repository/postgres/idempotency_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`

**Interfaces:**
- Consumes: `IdempotencyStore` exactly as declared in the interface ledger and `domain.IdempotencyRecord`.
- Produces: `requireIdempotency(next http.Handler) http.Handler`; deterministic replay for successful create requests.

- [ ] **Step 1: Write replay, conflict and in-progress tests**

```go
func TestIdempotencyReplaysSuccessfulResponse(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)
	first := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	second := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	require.Equal(t, http.StatusCreated, first.Code)
	require.Equal(t, first.Body.String(), second.Body.String())
	require.Equal(t, 1, calls)
}

func TestIdempotencyRejectsSameKeyWithDifferentBody(t *testing.T) {
	handler := authenticatedIdempotentHandler(t, successfulCreateHandler())
	_ = requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	conflict := requestWithKey(t, handler, "key-1", `{"purpose":"body"}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), `"code":"idempotency_conflict"`)
}
```

- [ ] **Step 2: Run the middleware tests and verify duplicate calls execute twice**

Run:

```bash
cd apps/server && go test ./internal/httpapi -run 'TestIdempotency' -v
```

Expected: FAIL because the middleware is undefined; before wiring, the replay test observes `calls == 2`.

- [ ] **Step 3: Implement canonical fingerprinting and atomic acquisition**

Read at most 1 MiB, decode JSON with `UseNumber`, recursively marshal objects with stable key ordering, then hash `METHOD + "\n" + escaped path + "\n" + canonical body` using SHA-256. Require a non-empty `Idempotency-Key` of at most 128 visible ASCII characters on create routes.

`BeginIdempotency` performs one `INSERT ... ON CONFLICT DO NOTHING`; on conflict it reads the row by `(user_id,key)`:

- different fingerprint → `conflict`;
- same fingerprint and `in_progress` → `in_progress`;
- same fingerprint and `completed` → `replay`.

Only 2xx responses call `CompleteIdempotency`; 4xx/5xx and panics call `AbortIdempotency`, allowing a corrected retry with the same fingerprint. Expiry is 24 hours.

- [ ] **Step 4: Wire create routes and complete the public error envelope**

Apply the middleware to `POST /v1/media/upload-intents` and `POST /v1/media/upload-intents/{id}/complete`; later plans must use the same wrapper for every new POST create route. Add `retryable bool` to every error response:

```json
{"error":{"code":"idempotency_in_progress","message":"相同请求仍在处理中，请稍后重试","request_id":"uuid","retryable":true}}
```

Use HTTP 409 for `idempotency_conflict` and `idempotency_in_progress`; mismatch is not retryable, in-progress is retryable.

- [ ] **Step 5: Verify concurrent acquisition and response replay**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/httpapi ./internal/repository/postgres -run 'Idempotency' -race -count=1
```

Expected: PASS; 20 concurrent identical requests execute the wrapped handler once, 19 receive replay or in-progress, and no duplicate side effect is written.

**Commit suggestion:**

```bash
git add contracts/openapi.yaml apps/server/internal/domain/idempotency.go apps/server/internal/httpapi apps/server/internal/repository/postgres/idempotency.go apps/server/internal/repository/postgres/idempotency_test.go
git commit -m "feat(server): enforce create request idempotency"
```

## Task 6: Implement Task Claim, Lease, Heartbeat and CAS Persistence

**Files:**
- Create: `apps/server/internal/repository/postgres/tasks.go`
- Create: `apps/server/internal/repository/postgres/tasks_test.go`
- Modify: `apps/server/internal/repository/postgres/store.go`

**Interfaces:**
- Consumes: `domain.Task`, `domain.TaskLease`, `domain.TaskFailure` and `taskrunner.TaskStore`.
- Produces: `postgres.Store.Claim`, `Heartbeat`, `Fail`; SQL contract for future Handler `Commit` methods to validate `tasks.id + lease_token + lease_owner + status='leased'`.

- [ ] **Step 1: Write real-Postgres concurrency tests**

```go
func TestClaimUsesSkipLockedAndClaimsEachTaskOnce(t *testing.T) {
	store, pool := newTaskStore(t)
	enqueueTasks(t, pool, 20, "assessment")
	var claimed sync.Map
	var group errgroup.Group
	for worker := 0; worker < 8; worker++ {
		owner := fmt.Sprintf("worker-%d", worker)
		group.Go(func() error {
			for {
				lease, ok, err := store.Claim(context.Background(), owner, 30*time.Second, []domain.TaskType{"assessment"})
				if err != nil || !ok {
					return err
				}
				if _, loaded := claimed.LoadOrStore(lease.ID, true); loaded {
					return fmt.Errorf("task %s claimed twice", lease.ID)
				}
			}
		})
	}
	require.NoError(t, group.Wait())
	require.Equal(t, 20, mapSize(&claimed))
}

func TestExpiredLeaseIsReclaimedAndOldTokenCannotWrite(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	oldLease, ok, err := store.Claim(context.Background(), "old", time.Second, []domain.TaskType{"assessment"})
	require.NoError(t, err)
	require.True(t, ok)
	advanceLeaseExpiry(t, pool, taskID)
	newLease, ok, err := store.Claim(context.Background(), "new", 30*time.Second, []domain.TaskType{"assessment"})
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, oldLease.LeaseToken, newLease.LeaseToken)
	updated, err := store.Heartbeat(context.Background(), oldLease, 30*time.Second)
	require.NoError(t, err)
	require.False(t, updated)
}
```

- [ ] **Step 2: Run task adapter tests and verify lease behavior is absent**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestClaim|TestExpiredLease' -v
```

Expected: FAIL to compile because `Store.Claim` and `Store.Heartbeat` do not exist.

- [ ] **Step 3: Implement one-statement Claim**

Claim order is `priority DESC, available_at, created_at`. Eligible rows are:

- `queued|retry_wait` with `available_at <= now()`;
- `leased` with `lease_expires_at <= now()`.

Skip rows with cancellation requested or `attempt >= max_attempts`. Use a CTE with `FOR UPDATE SKIP LOCKED LIMIT 1`, then update status to `leased`, increment attempt, generate a new lease token, set owner/expires/heartbeat, and return the row in the same statement. The `types` slice only contains Registry types and is passed as `text[]`; no task-type CHECK is added.

```sql
WITH candidate AS (
  SELECT id
  FROM tasks
  WHERE type = ANY($1::text[])
    AND cancel_requested_at IS NULL
    AND attempt < max_attempts
    AND (
      (status IN ('queued','retry_wait') AND available_at <= now())
      OR (status='leased' AND lease_expires_at <= now())
    )
  ORDER BY priority DESC, available_at, created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE tasks t
SET status='leased', attempt=t.attempt+1, lease_token=gen_random_uuid(),
    lease_owner=$2, lease_expires_at=now()+$3::interval,
    heartbeat_at=now(), updated_at=now()
FROM candidate
WHERE t.id=candidate.id
RETURNING t.*;
```

- [ ] **Step 4: Implement lease-token guarded heartbeat and failure**

Heartbeat updates only when `id`, `lease_token`, `lease_owner`, `status='leased'`, and `lease_expires_at > now()` match. Failure clears lease fields and:

- moves retryable errors to `retry_wait` with caller-computed `available_at`;
- moves permanent, exhausted, quality-rejected errors to `failed`;
- moves superseded errors to `superseded`.

Every method returns `updated=false` on a stale lease; callers translate this to `repository.ErrLeaseLost`, never overwrite the newer worker.

- [ ] **Step 5: Run race and lease tests**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'Task|Claim|Lease|Heartbeat' -race -count=1
```

Expected: PASS; each task is claimed once per active lease, expired tasks receive a new token, and old-token heartbeat/failure affects zero rows.

**Commit suggestion:**

```bash
git add apps/server/internal/domain/task.go apps/server/internal/repository/postgres/tasks.go apps/server/internal/repository/postgres/tasks_test.go
git commit -m "feat(server): add leased postgres task queue"
```

## Task 7: Build the Registry-Driven Worker Runner

**Files:**
- Create: `apps/server/internal/service/taskrunner/ports.go`
- Create: `apps/server/internal/service/taskrunner/registry.go`
- Create: `apps/server/internal/service/taskrunner/registry_test.go`
- Create: `apps/server/internal/service/taskrunner/runner.go`
- Create: `apps/server/internal/service/taskrunner/runner_test.go`

**Interfaces:**
- Consumes: `taskrunner.TaskStore` and `taskrunner.Handler` exactly as declared in the interface ledger.
- Produces: `taskrunner.Definition{Type,MaxAttempts,Timeout,LeaseDuration,HeartbeatEvery,Concurrency,RetryBackoff}`; `taskrunner.NewRegistry(definitions,handlers)`; `taskrunner.Runner.Run(ctx)`.

- [ ] **Step 1: Write Registry and stale-worker tests**

```go
func TestRegistryRejectsDuplicateAndMissingHandlers(t *testing.T) {
	def := taskrunner.Definition{
		Type: "assessment", MaxAttempts: 3, Timeout: 90*time.Second,
		LeaseDuration: 30*time.Second, HeartbeatEvery: 10*time.Second,
		Concurrency: 2, RetryBackoff: taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
	_, err := taskrunner.NewRegistry([]taskrunner.Definition{def, def}, []taskrunner.Handler{fakeHandler{taskType: "assessment"}})
	require.ErrorContains(t, err, "duplicate task definition")
	_, err = taskrunner.NewRegistry([]taskrunner.Definition{def}, nil)
	require.ErrorContains(t, err, "missing handler")
}

func TestRunnerDoesNotCommitAfterLeaseLoss(t *testing.T) {
	store := &taskStoreFake{heartbeatResults: []bool{true, false}}
	handler := &blockingHandler{taskType: "assessment"}
	runner := taskrunner.NewRunner(store, oneTypeRegistry(handler), "worker-1", 10*time.Millisecond, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)
	require.Eventually(t, func() bool { return handler.executeCancelled.Load() }, time.Second, 10*time.Millisecond)
	require.Zero(t, handler.commitCalls.Load())
}
```

- [ ] **Step 2: Run runner tests and verify the old loop cannot satisfy the contract**

Run:

```bash
cd apps/server && go test ./internal/service/taskrunner -run 'TestRegistry|TestRunner' -race -v
```

Expected: FAIL because the package is absent.

- [ ] **Step 3: Implement Registry as the single policy source**

Validate on construction:

- exactly one Definition and one Handler per TaskType;
- `MaxAttempts > 0`;
- `Timeout > HeartbeatEvery`;
- `LeaseDuration >= 2 * HeartbeatEvery`;
- `Concurrency > 0`;
- no unregistered Handler.

Registry exposes immutable `Types()`, `Definition(type)` and `Handler(type)`. Enqueue use cases in later plans must read `MaxAttempts` from this Registry; no retry-count map may exist elsewhere.

- [ ] **Step 4: Implement bounded execution, heartbeat and typed retry**

Runner claims only when a per-type semaphore has capacity. `Execute` receives a context bounded by Definition timeout. Start a heartbeat ticker only after claim; on heartbeat false/error, cancel execution and skip Commit/Fail. On success call `Handler.Commit`; the Handler’s domain repository transaction must atomically validate Task lease and target generation, write the immutable result/current pointer, and set Task/Operation terminal state. `CommitSuperseded` is a successful stale-result rejection, not a retry.

On execution error, obtain `domain.TaskError` with class/code. Retry only `transient|throttled` while `attempt < max_attempts`; compute retry time from the Definition callback. Panic recovery maps to `transient/handler_panic`, logs Task ID and trace ID, and never logs payload.

- [ ] **Step 5: Verify runner cancellation, policy and race safety**

Run:

```bash
cd apps/server && go test ./internal/service/taskrunner -race -count=1 -v
```

Expected: PASS; concurrency never exceeds each Definition limit, heartbeat loss cancels work, one panic is isolated, and retry scheduling uses only Registry policy.

**Commit suggestion:**

```bash
git add apps/server/internal/service/taskrunner
git commit -m "feat(server): add registry driven task runner"
```

## Task 8: Record Every Provider Invocation Safely

**Files:**
- Create: `apps/server/internal/repository/postgres/provider_invocations.go`
- Create: `apps/server/internal/repository/postgres/provider_invocations_test.go`
- Create: `apps/server/internal/provider/ai/invocations.go`
- Create: `apps/server/internal/provider/ai/invocations_test.go`

**Interfaces:**
- Consumes: `InvocationStore` from the interface ledger; `domain.StartInvocation`; `domain.FinishInvocation`.
- Produces: `ai.RecordInvocation(ctx,input,call)` returning provider result plus immutable `InvocationMeta{InvocationID,ProviderRequestID,LatencyMS,EstimatedCostCNY}`.

- [ ] **Step 1: Write success, failure and redaction tests**

```go
func TestRecordInvocationFinishesFailureWithoutSensitiveInput(t *testing.T) {
	store := &invocationStoreFake{}
	recorder := ai.NewInvocationRecorder(store, fixedClock())
	_, meta, err := recorder.Record(context.Background(), domain.StartInvocation{
		UserID: "u1", OperationID: "op1", TaskID: "task1", AttemptNo: 1,
		Capability: "photo_quality_check", RoutingConfigVersion: "route-v1",
		ProviderKey: "primary", ModelKey: "vision-main", Protocol: "openai_images",
		RequestHash: sha256.Sum256([]byte("redacted canonical request")),
		InputImages: 3,
	}, func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{ProviderRequestID: "req-1"}, ai.NewCallError(domain.ErrorThrottled, "rate_limited")
	})
	require.Error(t, err)
	require.Equal(t, domain.InvocationFailed, store.finished.Status)
	require.Equal(t, domain.ErrorThrottled, store.finished.ErrorClass)
	require.NotContains(t, string(store.serialized), "image/jpeg;base64")
	require.NotContains(t, string(store.serialized), "https://private-cos")
	require.Equal(t, meta.InvocationID, store.finished.InvocationID)
}
```

- [ ] **Step 2: Run invocation tests and verify no ledger decorator exists**

Run:

```bash
cd apps/server && go test ./internal/provider/ai ./internal/repository/postgres -run 'Invocation' -v
```

Expected: FAIL because `provider/ai` invocation recorder and adapter methods are absent.

- [ ] **Step 3: Implement start/finish persistence**

`StartInvocation` inserts status `running` before the external call and returns the generated ID. `FinishInvocation` updates only rows matching `(user_id,id,task_id,attempt_no,status='running')`, records status, tokens/images, estimated cost, latency, typed error and finish time. An update of zero rows returns `repository.ErrConflict`.

Indexes support `operation_id`, `task_id`, `(capability,started_at)` and `provider_request_id` when non-null. Do not persist request/response bodies, signed URLs, image bytes, auth headers or embeddings.

- [ ] **Step 4: Implement the provider-side decorator**

The decorator hashes a caller-supplied canonical redacted request, starts the row, times exactly one Provider network call, classifies errors by typed `ai.CallError`, then finishes the row even when the parent context is cancelled by using a fresh 3-second write context. It returns the invocation ID to the business Handler so immutable outputs can reference it.

- [ ] **Step 5: Verify ledger completeness**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/provider/ai ./internal/repository/postgres -run 'Invocation' -race -count=1
```

Expected: PASS; success, Provider failure and context cancellation each leave one finished invocation row linked to the exact Operation, Task and attempt.

**Commit suggestion:**

```bash
git add apps/server/internal/domain/invocation.go apps/server/internal/provider/ai apps/server/internal/repository/postgres/provider_invocations.go apps/server/internal/repository/postgres/provider_invocations_test.go
git commit -m "feat(server): add provider invocation ledger"
```

## Task 9: Add Operation-Scoped Billing Reservations

**Files:**
- Create: `apps/server/internal/service/billing/ports.go`
- Create: `apps/server/internal/service/billing/service.go`
- Create: `apps/server/internal/service/billing/service_test.go`
- Modify: `apps/server/internal/domain/billing.go`
- Create: `apps/server/internal/repository/postgres/billing.go`
- Create: `apps/server/internal/repository/postgres/billing_test.go`

**Interfaces:**
- Consumes: billing Repository interface from the interface ledger; `domain.ReserveBilling`, `SettleBilling`, `RefundBilling`.
- Produces: `billing.Service.Reserve`, `Settle`, `Refund`; one reservation per Operation; idempotent ledger entries.

- [ ] **Step 1: Write reservation lifecycle tests**

```go
func TestReservationLifecycleIsIdempotent(t *testing.T) {
	repo, pool := newBillingRepo(t, 3)
	service := billing.New(repo)
	first, created, err := service.Reserve(context.Background(), domain.ReserveBilling{
		UserID: "u1", OperationID: "op1", Kind: "render", Units: 1,
	})
	require.NoError(t, err)
	require.True(t, created)
	second, created, err := service.Reserve(context.Background(), domain.ReserveBilling{
		UserID: "u1", OperationID: "op1", Kind: "render", Units: 1,
	})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 2, walletCredits(t, pool, "u1"))
	_, settled, err := service.Settle(context.Background(), domain.SettleBilling{
		UserID: "u1", OperationID: "op1", ResultType: "render_publication", ResultID: "pub1",
	})
	require.NoError(t, err)
	require.True(t, settled)
	_, refunded, err := service.Refund(context.Background(), domain.RefundBilling{
		UserID: "u1", OperationID: "op1", Reason: "failed",
	})
	require.ErrorIs(t, err, billing.ErrAlreadySettled)
	require.False(t, refunded)
}

func TestRefundReturnsCreditsExactlyOnce(t *testing.T) {
	repo, pool := newBillingRepo(t, 1)
	service := billing.New(repo)
	_, _, _ = service.Reserve(context.Background(), domain.ReserveBilling{UserID: "u1", OperationID: "op1", Kind: "assessment", Units: 1})
	_, changed, err := service.Refund(context.Background(), domain.RefundBilling{UserID: "u1", OperationID: "op1", Reason: "cancelled"})
	require.NoError(t, err)
	require.True(t, changed)
	_, changed, err = service.Refund(context.Background(), domain.RefundBilling{UserID: "u1", OperationID: "op1", Reason: "cancelled"})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, 1, walletCredits(t, pool, "u1"))
}
```

- [ ] **Step 2: Run billing tests and verify Task-scoped charging fails the new contract**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/service/billing ./internal/repository/postgres -run 'Reservation|Refund' -v
```

Expected: FAIL because Operation-scoped reservation APIs do not exist.

- [ ] **Step 3: Implement atomic Reserve**

In one transaction:

1. lock `billing_wallets` by user;
2. verify the referenced Operation belongs to that user;
3. return the existing reservation if `(user_id,operation_id)` exists and kind/units match;
4. reject same Operation with different kind/units as conflict;
5. require sufficient credits;
6. decrement wallet;
7. insert `billing_reservations(status='reserved')`;
8. insert one ledger row `reason='reserve', delta=-units, reference_type='operation', reference_id=operation_id`.

Return `billing.ErrInsufficientCredits` without changing any row.

- [ ] **Step 4: Implement terminal Settle and Refund CAS**

`Settle` changes only `reserved → settled`, stores immutable `result_type/result_id`, and adds a zero-delta `settle` ledger row. It is idempotent only when the same result reference is repeated.

`Refund` changes only `reserved → refunded`, increments wallet once, and adds a `refund` ledger row with `delta=+units`. Only Operation terminal reasons `failed|cancelled|superseded` are accepted. A settled reservation cannot be refunded; a refunded reservation cannot be settled. Candidate 2 and network retry Tasks never call Reserve.

- [ ] **Step 5: Verify concurrent Reserve/Refund**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/service/billing ./internal/repository/postgres -run 'Billing|Reservation|Refund|Settle' -race -count=1
```

Expected: PASS; 20 concurrent Reserve calls deduct once, concurrent Refund calls return credits once, and settlement remains bound to the final Report/PlanSet/Publication reference rather than a Task ID.

**Commit suggestion:**

```bash
git add apps/server/internal/domain/billing.go apps/server/internal/service/billing apps/server/internal/repository/postgres/billing.go apps/server/internal/repository/postgres/billing_test.go
git commit -m "feat(server): add operation billing reservations"
```

## Task 10: Separate API and Worker Bootstrap

**Files:**
- Create: `apps/server/internal/bootstrap/api.go`
- Create: `apps/server/internal/bootstrap/api_test.go`
- Create: `apps/server/internal/bootstrap/worker.go`
- Create: `apps/server/internal/bootstrap/worker_test.go`
- Modify: `apps/server/internal/config/config.go`
- Modify: `apps/server/internal/config/config_test.go`
- Modify: `apps/server/cmd/api/main.go`
- Modify: `apps/server/cmd/worker/main.go`
- Modify: `apps/server/Dockerfile`

**Interfaces:**
- Consumes: `taskrunner.NewRegistry`, `taskrunner.NewRunner`, media/operation/billing services and Postgres Store.
- Produces: `bootstrap.BuildAPI(cfg,logger) (*bootstrap.APIApp,error)` and `bootstrap.BuildWorker(cfg,logger) (*bootstrap.WorkerApp,error)`；`APIApp` exposes `Handler`/`Close`，`WorkerApp` exposes `Run(ctx)`/`Close`。

- [ ] **Step 1: Write process-boundary tests**

```go
func TestBuildAPIDoesNotConstructRunner(t *testing.T) {
	deps := bootstrap.Dependencies{RunnerFactory: func(taskrunner.Options) (*taskrunner.Runner, error) {
		t.Fatal("API must not construct worker runner")
		return nil, nil
	}}
	app, err := bootstrap.BuildAPIWithDependencies(testConfig(), slog.Default(), deps)
	require.NoError(t, err)
	require.NotNil(t, app.Handler)
	app.Close()
}

func TestWorkerConfigRequiresStableOwnerAndHeartbeatRatio(t *testing.T) {
	cfg := testConfig()
	cfg.WorkerID = ""
	require.ErrorContains(t, config.Validate(cfg), "WORKER_ID")
	cfg.WorkerID = "worker-a"
	cfg.TaskLeaseDuration = 20 * time.Second
	cfg.TaskHeartbeatEvery = 15 * time.Second
	require.ErrorContains(t, config.Validate(cfg), "lease duration")
}
```

- [ ] **Step 2: Run bootstrap tests and verify API still embeds Worker**

Run:

```bash
cd apps/server && go test ./internal/bootstrap ./internal/config -run 'BuildAPI|WorkerConfig' -v
```

Expected: FAIL because `BuildAPI`/`BuildWorker` do not exist and current API conditionally calls `RunWorker`.

- [ ] **Step 3: Add explicit runtime configuration**

Replace `RunWorker` and `AnalysisPollTime` with:

```go
WorkerID string
TaskPollInterval time.Duration
TaskLeaseDuration time.Duration
TaskHeartbeatEvery time.Duration
```

Environment names and defaults:

```text
WORKER_ID=<required only by cmd/worker>
TASK_POLL_INTERVAL_MS=500
TASK_LEASE_DURATION_SECONDS=30
TASK_HEARTBEAT_SECONDS=10
```

API configuration validation ignores `WORKER_ID`; Worker validation requires it. Both require lease duration at least twice heartbeat interval. Production has no `RUN_WORKER` switch.

- [ ] **Step 4: Move dependency assembly into separate bootstrap functions**

`cmd/api/main.go` loads config, calls `BuildAPI`, starts HTTP and shuts down within 10 seconds. It never imports `service/taskrunner`.

`cmd/worker/main.go` loads config, calls `BuildWorker`, and blocks in `runner.Run(ctx)`. Worker bootstrap creates the Registry and registers only concrete handlers available at that implementation point; it fails startup when a Definition lacks a Handler. API and Worker both run the embedded migration safely, share Postgres/COS configuration, and have independent cleanup.

The Dockerfile builds `/app/uplook-api` and `/app/uplook-worker` in one build stage, copies both into the non-root image, and leaves `ENTRYPOINT ["/app/uplook-api"]` as the default that Compose can override for Worker.

- [ ] **Step 5: Verify binaries and process separation**

Run:

```bash
cd apps/server
go test ./internal/bootstrap ./internal/config -count=1
CGO_ENABLED=0 go build -o /tmp/uplook-api ./cmd/api
CGO_ENABLED=0 go build -o /tmp/uplook-worker ./cmd/worker
```

Expected: PASS; both binaries build, static import inspection confirms `cmd/api` does not import taskrunner, and the API bootstrap test proves no Runner is constructed.

**Commit suggestion:**

```bash
git add apps/server/internal/bootstrap apps/server/internal/config apps/server/cmd apps/server/Dockerfile
git commit -m "refactor(server): separate api and worker bootstrap"
```

## Task 11: Verify Separate API and Worker Binaries

**Authoritative replacement:** 下方修改 `Makefile/docker-compose.yml`、Compose smoke test 和 root commit 的旧步骤全部跳过。Foundation 只执行：

```bash
cd apps/server
CGO_ENABLED=0 go build -o /tmp/uplook-api ./cmd/api
CGO_ENABLED=0 go build -o /tmp/uplook-worker ./cmd/worker
test -x /tmp/uplook-api
test -x /tmp/uplook-worker
cd ../..
git diff --exit-code -- Makefile docker-compose.yml
```

Expected: 两个二进制构建成功且根文件无差异。Task 11 不创建提交；Compose 独立服务由最终 Peripherals/Cutover 唯一 root task 完成。

**Files:**
- Verify: `apps/server/cmd/api/main.go`
- Verify: `apps/server/cmd/worker/main.go`
- Verify: `apps/server/Dockerfile`

**Interfaces:**
- Consumes: APIApp/WorkerApp and the two command entries from Task 10.
- Produces: direct build evidence only; root orchestration remains untouched.

## Task 12: Add Foundation Acceptance and Traceability Tests

**Files:**
- Create: `apps/server/internal/bootstrap/foundation_integration_test.go`
- Modify: `contracts/openapi.yaml`

**Interfaces:**
- Consumes: all interfaces and vertical slices produced by Tasks 1–11.
- Produces: one acceptance suite proving ownership, idempotency, lease recovery, invocation traceability and billing refund invariants.

- [ ] **Step 1: Write the end-to-end foundation scenario**

The Go integration test creates two users and executes this exact sequence:

1. create and complete one UploadIntent for user A;
2. replay completion and assert the same Asset;
3. query user A’s Operation and reject user B with not found;
4. enqueue one Task linked to that Operation;
5. claim with Worker A, expire lease, reclaim with Worker B;
6. record Provider invocation for Worker B’s attempt;
7. attempt stale Worker A commit and assert zero business/task rows changed;
8. commit Worker B using Task lease + subject generation CAS;
9. reserve one billing unit by Operation, then refund on a failed terminal Operation;
10. query by Operation trace and assert the Operation, Task and invocation IDs form one complete chain.

```go
func TestFoundationTraceSurvivesLeaseRecovery(t *testing.T) {
	env := newFoundationEnvironment(t)
	op := env.CreateOperation("user-a", "assessment")
	task := env.EnqueueTask(op, "assessment", 7)
	oldLease := env.Claim("worker-a")
	env.Expire(oldLease)
	newLease := env.Claim("worker-b")
	invocation := env.RecordInvocation(newLease)
	require.ErrorIs(t, env.Commit(oldLease, 7), repository.ErrLeaseLost)
	require.NoError(t, env.Commit(newLease, 7))
	trace := env.Trace(op.ID)
	require.Equal(t, task.ID, trace.Task.ID)
	require.Equal(t, invocation.ID, trace.Invocation.ID)
	require.Equal(t, newLease.Attempt, trace.Invocation.AttemptNo)
	require.Equal(t, domain.OperationSucceeded, trace.Operation.Status)
}
```

- [ ] **Step 2: Run the acceptance test before final wiring**

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/bootstrap -run 'TestFoundationTrace' -race -v
```

Expected: FAIL until all final Task/Operation/invocation transaction wiring is connected; the expected failure is `operation remains running` or `task lease was not committed`.

- [ ] **Step 3: Complete atomic trace transitions**

Add no new abstraction. Wire the existing Handler `Commit` contract so its concrete repository transaction:

- checks `tasks.id`, user, lease token, owner and `status='leased'`;
- checks target `subject_generation`;
- writes the immutable result/current pointer;
- marks Task `succeeded` or `superseded`;
- updates Operation result/status/progress to `succeeded/10000` or `superseded`;
- commits all changes together.

Wire terminal `failed|cancelled|superseded` Operation transitions to `billing.Service.Refund`; wire successful publication-oriented use cases in later plans to `billing.Service.Settle`.

- [ ] **Step 4: Add the HTTP foundation scenario to the Go acceptance harness**

In `foundation_integration_test.go`, start `httptest.Server` with the real router, a fake `media.ObjectStore` that returns a deterministic signed PUT grant/HEAD result, and the real temporary PostgreSQL adapters. Create UploadIntent, complete it, query `/v1/operations/{id}`, verify `/v1/tasks/{id}` is 404, repeat creates with the same `Idempotency-Key`, and assert the returned resource ID is unchanged. This test process owns the fake store; production and Compose still use COS, and no multipart or local-upload compatibility endpoint is introduced.

- [ ] **Step 5: Run all in-scope verification**

Run:

```bash
docker compose up -d postgres
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./... -race -count=1
go vet ./...
cd ../..
node contracts/scripts/check-sync.mjs
make server-build
docker compose config --quiet
```

Expected: every command exits 0; concurrent duplicate requests create one side effect, old lease tokens cannot commit, cross-user resource attachment/read fails, each Provider call has a traceable invocation, and API/Worker build independently.

**Commit suggestion:**

```bash
git add apps/server/internal/bootstrap/foundation_integration_test.go contracts/openapi.yaml
git commit -m "test(server): cover quality core foundation invariants"
```

## Implementation Completion Checklist

- Baseline contains all quality-core entity tables from the approved design and no historical migration SQL.
- Every user-owned row and parent-child relationship enforces tenant ownership in PostgreSQL.
- Domain foundation types live in focused files; no migrated type remains duplicated in `domain.go`.
- Upload completion trusts COS HEAD metadata, not client completion claims.
- Public API exposes Operation only; Task payload, lease, Provider and internal scores are absent.
- Idempotency prevents both serial and concurrent duplicate side effects.
- Task claim uses `SKIP LOCKED`; heartbeat and all writes validate the current lease token.
- Handler Commit contract requires lease CAS and subject generation CAS in the same transaction.
- Retry budget, timeout, heartbeat and concurrency are defined only in Registry.
- Provider Invocation rows cover success, failure and cancellation without sensitive payloads.
- Billing reserves once per Operation, settles on immutable result and refunds once on failure/cancel/supersede.
- API never starts Worker; Worker has an independent binary and Compose service.
- Root frozen files are not changed by Foundation; the sole root task is in Peripherals/Cutover.
- No microservice, Redis, Kafka, event sourcing, workflow engine or compatibility layer is introduced.
- No placeholder text or unspecified follow-up remains in this plan.
