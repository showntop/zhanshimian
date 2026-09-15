package hair_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/hair"
	"github.com/zhanshimian/server/internal/service/rendering"
	"github.com/zhanshimian/server/internal/testutil"
)

// handler_integration_test.go 在真实 PostgreSQL 上驱动 hair_preview 全链路：
// 创建（预览行+Operation+任务单事务）→ worker 领取 → Execute → Commit，
// 成功/失败两态；对象库与 hair_edit 生成器换成确定性 fake（生成器仍落
// provider_invocations 台账行，媒体外键与生产同形）。

type hairEnv struct {
	t         *testing.T
	pool      *pgxpool.Pool
	store     *postgres.Store
	objects   *memoryObjects
	svc       *hair.Service
	handler   *hair.Handler
	generator *fakeGenerator
	userID    string
}

func newHairEnv(t *testing.T) *hairEnv {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	objects := newMemoryObjects()
	userID := insertUser(t, pool)
	generator := &fakeGenerator{pool: pool, userID: userID}
	svc := hair.New(store, store, store, store, testSigner{})
	handler := hair.NewHandler(store, objects, objectSourceLoader{objects: objects},
		storeProgress{store: store}, generator, renderingNormalizer{})
	env := &hairEnv{t: t, pool: pool, store: store, objects: objects, svc: svc, handler: handler, generator: generator, userID: userID}
	return env
}

func insertUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users(nickname) VALUES ('hair-e2e') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func (e *hairEnv) insertFaceAsset() string {
	e.t.Helper()
	key := "users/" + e.userID + "/uploads/" + uuid.NewString() + ".jpg"
	data := testJPEG(e.t, 600, 800)
	if _, err := e.objects.Save(context.Background(), key, bytes.NewReader(data)); err != nil {
		e.t.Fatalf("save object: %v", err)
	}
	sum := sha256.Sum256(data)
	var id string
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, width, height, state, display_kind)
		VALUES ($1::uuid,'user_upload','face',$2,$3,'image/jpeg',$4,600,800,'ready','original')
		RETURNING id::text`, e.userID, key, hex.EncodeToString(sum[:]), len(data)).Scan(&id)
	if err != nil {
		e.t.Fatalf("insert face asset: %v", err)
	}
	return id
}

func (e *hairEnv) createPreview(mediaAssetID string) hair.Preview {
	e.t.Helper()
	preview, operation, err := e.svc.CreatePreview(context.Background(), e.userID, hair.CreatePreviewInput{
		MediaAssetID: mediaAssetID, StyleID: uuid.NewString(),
	})
	if err != nil {
		e.t.Fatalf("create preview: %v", err)
	}
	if operation.Status != domain.OperationAccepted {
		e.t.Fatalf("operation status = %s, want accepted", operation.Status)
	}
	return preview
}

func (e *hairEnv) claim() domain.TaskLease {
	e.t.Helper()
	lease, ok, err := e.store.Claim(context.Background(), "hair-e2e-worker", 30*time.Second, []domain.TaskType{hair.TaskTypeHairPreview})
	if err != nil || !ok {
		e.t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	return lease
}

func (e *hairEnv) runTaskSuccess(lease domain.TaskLease) domain.TaskResult {
	e.t.Helper()
	e.generator.operationID, e.generator.taskID = lease.OperationID, lease.ID
	result, err := e.handler.Execute(context.Background(), lease)
	if err != nil {
		e.t.Fatalf("execute: %v", err)
	}
	outcome, err := e.handler.Commit(context.Background(), lease, result)
	if err != nil || outcome != domain.CommitApplied {
		e.t.Fatalf("commit: outcome=%s err=%v", outcome, err)
	}
	return result
}

func (e *hairEnv) operation(id string) domain.Operation {
	e.t.Helper()
	op, err := e.store.GetOperation(context.Background(), e.userID, id)
	if err != nil {
		e.t.Fatalf("get operation: %v", err)
	}
	return op
}

func (e *hairEnv) taskState(operationID string) (status string, errorClass string) {
	e.t.Helper()
	var klass *string
	err := e.pool.QueryRow(context.Background(), `
		SELECT status, error_class FROM tasks WHERE user_id=$1::uuid AND operation_id=$2::uuid`,
		e.userID, operationID).Scan(&status, &klass)
	if err != nil {
		e.t.Fatalf("task state: %v", err)
	}
	if klass != nil {
		errorClass = *klass
	}
	return status, errorClass
}

// ---- 链路：创建即落任务（P0 回归钉）+ 成功态 ----

func TestHairPreviewChainCreatesTaskAndPublishes(t *testing.T) {
	env := newHairEnv(t)
	faceID := env.insertFaceAsset()
	preview := env.createPreview(faceID)

	// 创建同事务落任务：worker 有可消费行（此前只有 operations 行 → 无人消费）。
	var taskCount int
	if err := env.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM tasks
		WHERE user_id=$1::uuid AND type='hair_preview' AND subject_id=$2::uuid AND status='queued'`,
		env.userID, preview.ID).Scan(&taskCount); err != nil || taskCount != 1 {
		t.Fatalf("queued hair_preview task count = %d err=%v, want 1", taskCount, err)
	}

	lease := env.claim()
	result := env.runTaskSuccess(lease)
	if result.Disposition != domain.TaskPublish || result.ResultType != "hair_preview" {
		t.Fatalf("result = %#v, want publish hair_preview", result)
	}

	op := env.operation(preview.Operation.ID)
	if op.Status != domain.OperationSucceeded || op.ProgressBPS != 10000 {
		t.Fatalf("operation = %s/%d, want succeeded/10000", op.Status, op.ProgressBPS)
	}
	if op.ResultType != "hair_preview" || op.ResultID != preview.ID {
		t.Fatalf("operation result = %s/%s, want hair_preview/%s", op.ResultType, op.ResultID, preview.ID)
	}
	if status, _ := env.taskState(preview.Operation.ID); status != string(domain.TaskSucceeded) {
		t.Fatalf("task status = %s, want succeeded", status)
	}

	// 结果媒体：provider_output + published + 风格参考角标，读模型即时签名。
	loaded, err := env.svc.Get(context.Background(), env.userID, preview.ID)
	if err != nil {
		t.Fatalf("get preview: %v", err)
	}
	if loaded.State != hair.StateReady {
		t.Fatalf("preview state = %s, want ready", loaded.State)
	}
	if loaded.Media == nil {
		t.Fatal("ready preview must carry result media")
	}
	if loaded.Media.SourceKind != "generated_preview" || loaded.Media.DisplayLabel != "风格参考" {
		t.Fatalf("result media badge = %s/%s, want generated_preview/风格参考",
			loaded.Media.SourceKind, loaded.Media.DisplayLabel)
	}
	if loaded.Media.MIMEType != "image/jpeg" {
		t.Fatalf("result media mime = %s, want image/jpeg", loaded.Media.MIMEType)
	}
	if loaded.Media.URL == "" || loaded.Media.URLExpiresAt.IsZero() {
		t.Fatalf("result media not signed: %#v", loaded.Media)
	}
	if loaded.SourceMedia == nil || loaded.SourceMedia.URL == "" || loaded.SourceMedia.SourceKind != "user_original" {
		t.Fatalf("source media = %#v, want signed user_original", loaded.SourceMedia)
	}
	var assetCount int
	if err := env.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM media_assets
		WHERE user_id=$1::uuid AND origin='provider_output' AND state='published'
		  AND display_kind='generated_reference' AND provider_invocation_id IS NOT NULL`,
		env.userID).Scan(&assetCount); err != nil || assetCount != 1 {
		t.Fatalf("provider_output asset count = %d err=%v, want 1", assetCount, err)
	}
}

// ---- 链路：transient 重试耗尽 → 终态失败（公开文案 + 可重试） ----

func TestHairPreviewChainTransientRetriesThenFails(t *testing.T) {
	env := newHairEnv(t)
	faceID := env.insertFaceAsset()
	preview := env.createPreview(faceID)
	env.generator.err = errors.New("upstream 5xx")

	for attempt := 1; attempt <= hair.TaskMaxAttempts; attempt++ {
		lease := env.claim()
		if lease.Attempt != attempt {
			t.Fatalf("attempt = %d, want %d", lease.Attempt, attempt)
		}
		if _, err := env.handler.Execute(context.Background(), lease); err == nil {
			t.Fatalf("attempt %d: execute must fail", attempt)
		}
		failure := domain.TaskFailure{Class: domain.ErrorTransient, Code: "hair_generate_failed"}
		if attempt < hair.TaskMaxAttempts {
			// runner 的退避重试路径：任务回 retry_wait，操作公开为重试中。
			updated, err := env.store.Fail(context.Background(), lease, failure, time.Now())
			if err != nil || !updated {
				t.Fatalf("fail for retry: updated=%v err=%v", updated, err)
			}
			continue
		}
		// 预算耗尽：runner 以 TaskDomainFail 收尾，由 handler.Commit 写总。
		outcome, err := env.handler.Commit(context.Background(), lease, domain.TaskResult{
			Disposition: domain.TaskDomainFail, Failure: &failure,
		})
		if err != nil || outcome != domain.CommitApplied {
			t.Fatalf("final commit: outcome=%s err=%v", outcome, err)
		}
	}

	op := env.operation(preview.Operation.ID)
	if op.Status != domain.OperationFailed {
		t.Fatalf("operation status = %s, want failed", op.Status)
	}
	if op.ErrorCode != "hair_generate_failed" {
		t.Fatalf("error code = %q, want hair_generate_failed", op.ErrorCode)
	}
	if op.PublicMessage == "" {
		t.Fatal("failed operation must carry a public message")
	}
	if !op.Retryable {
		t.Fatal("transient-exhausted failure must be retryable")
	}
	if op.TraceID == "" {
		t.Fatal("failed operation must carry a trace id (operations CHECK)")
	}
	status, class := env.taskState(preview.Operation.ID)
	if status != string(domain.TaskFailed) || class != string(domain.ErrorTransient) {
		t.Fatalf("task = %s/%s, want failed/transient", status, class)
	}
	loaded, err := env.svc.Get(context.Background(), env.userID, preview.ID)
	if err != nil {
		t.Fatalf("get preview: %v", err)
	}
	if loaded.State != hair.StateFailed || !loaded.Retryable {
		t.Fatalf("preview = %s/retryable=%v, want failed/true", loaded.State, loaded.Retryable)
	}
	if loaded.Media != nil {
		t.Fatalf("failed preview must not carry result media: %#v", loaded.Media)
	}
}

// ---- 链路：provider 输出不合契约 → quality_rejected 一次性失败 ----

func TestHairPreviewChainQualityRejectedWithoutRetry(t *testing.T) {
	env := newHairEnv(t)
	faceID := env.insertFaceAsset()
	preview := env.createPreview(faceID)
	env.generator.data = []byte("this is not an image")

	lease := env.claim()
	env.generator.operationID, env.generator.taskID = lease.OperationID, lease.ID
	result, err := env.handler.Execute(context.Background(), lease)
	if err != nil {
		t.Fatalf("quality rejection must be a domain result, not an error: %v", err)
	}
	if result.Disposition != domain.TaskDomainFail || result.Failure == nil ||
		result.Failure.Class != domain.ErrorQualityRejected || result.Failure.Code != "hair_output_invalid" {
		t.Fatalf("result = %#v, want quality_rejected hair_output_invalid", result)
	}
	outcome, err := env.handler.Commit(context.Background(), lease, result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("commit: outcome=%s err=%v", outcome, err)
	}

	op := env.operation(preview.Operation.ID)
	if op.Status != domain.OperationFailed || op.ErrorCode != "hair_output_invalid" {
		t.Fatalf("operation = %s/%s, want failed/hair_output_invalid", op.Status, op.ErrorCode)
	}
	if op.PublicMessage == "" || op.TraceID == "" {
		t.Fatalf("failed operation missing message/trace: %#v", op)
	}
	status, class := env.taskState(preview.Operation.ID)
	if status != string(domain.TaskFailed) || class != string(domain.ErrorQualityRejected) {
		t.Fatalf("task = %s/%s, want failed/quality_rejected", status, class)
	}
	loaded, err := env.svc.Get(context.Background(), env.userID, preview.ID)
	if err != nil {
		t.Fatalf("get preview: %v", err)
	}
	if loaded.State != hair.StateFailed || loaded.Media != nil {
		t.Fatalf("preview = %s media=%#v, want failed without media", loaded.State, loaded.Media)
	}
	// 一次性失败：任务不得回到可领取状态。
	if _, ok, err := env.store.Claim(context.Background(), "hair-e2e-worker", 30*time.Second, []domain.TaskType{hair.TaskTypeHairPreview}); err != nil || ok {
		t.Fatalf("rejected task must not be reclaimable: ok=%v err=%v", ok, err)
	}
}

// ---- 列表语义：无参含进行中（契约），saved/state 过滤 ----

func TestHairPreviewListIncludesInFlight(t *testing.T) {
	env := newHairEnv(t)
	faceID := env.insertFaceAsset()

	// 一条跑完并保存，一条保持进行中。
	done := env.createPreview(faceID)
	env.runTaskSuccess(env.claim())
	if _, err := env.svc.Save(context.Background(), env.userID, done.ID); err != nil {
		t.Fatalf("save preview: %v", err)
	}
	inFlight := env.createPreview(faceID)

	all, err := env.svc.List(context.Background(), env.userID, hair.ListFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered list = %d items, want 2 (含进行中)", len(all))
	}
	seen := map[string]bool{}
	for _, item := range all {
		seen[item.ID] = true
	}
	if !seen[inFlight.ID] || !seen[done.ID] {
		t.Fatalf("unfiltered list missing items: %v", seen)
	}

	savedOnly := true
	saved, err := env.svc.List(context.Background(), env.userID, hair.ListFilter{Saved: &savedOnly})
	if err != nil || len(saved) != 1 || saved[0].ID != done.ID {
		t.Fatalf("saved list = %#v err=%v, want only the saved one", saved, err)
	}

	active, err := env.svc.List(context.Background(), env.userID, hair.ListFilter{Active: true})
	if err != nil || len(active) != 1 || active[0].ID != inFlight.ID {
		t.Fatalf("active list = %#v err=%v, want only the in-flight one", active, err)
	}

	got, err := env.svc.Active(context.Background(), env.userID)
	if err != nil || got.ID != inFlight.ID {
		t.Fatalf("active preview = %#v err=%v, want %s", got, err, inFlight.ID)
	}
}

// ---- 创建校验：media_id 优先、空 report_id 不再 500、越权 404 ----

func TestCreatePreviewFaceResolution(t *testing.T) {
	env := newHairEnv(t)
	faceID := env.insertFaceAsset()

	// 空 report_id + 有效 media_id：客户端真实请求形状（review P0-4：
	// 旧实现把空串强转 uuid 必 500）。
	preview, _, err := env.svc.CreatePreview(context.Background(), env.userID, hair.CreatePreviewInput{
		MediaAssetID: faceID, StyleID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create with media_id and empty report_id: %v", err)
	}
	if preview.SourceMedia == nil || preview.SourceMedia.AssetID != faceID || preview.SourceMedia.SourceKind != "user_original" {
		t.Fatalf("source media = %#v, want the uploaded face", preview.SourceMedia)
	}

	// demo 照片按 demo_example 投影（红线：来源真实性）。
	demoKey := "demo/" + env.userID + "/face-" + uuid.NewString() + ".jpg"
	demoData := testJPEG(t, 600, 800)
	if _, err := env.objects.Save(context.Background(), demoKey, bytes.NewReader(demoData)); err != nil {
		t.Fatalf("save demo object: %v", err)
	}
	demoSum := sha256.Sum256(demoData)
	var demoID string
	if err := env.pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, width, height, state, display_kind)
		VALUES ($1::uuid,'demo','face',$2,$3,'image/jpeg',$4,600,800,'ready','effect_example')
		RETURNING id::text`, env.userID, demoKey, hex.EncodeToString(demoSum[:]), len(demoData)).Scan(&demoID); err != nil {
		t.Fatalf("insert demo asset: %v", err)
	}
	demoPreview, _, err := env.svc.CreatePreview(context.Background(), env.userID, hair.CreatePreviewInput{
		MediaAssetID: demoID, StyleID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create with demo media: %v", err)
	}
	if demoPreview.SourceMedia.SourceKind != "demo_example" || demoPreview.SourceMedia.DisplayLabel != "效果示例" {
		t.Fatalf("demo source = %s/%s, want demo_example/效果示例",
			demoPreview.SourceMedia.SourceKind, demoPreview.SourceMedia.DisplayLabel)
	}

	// 既无 media_id 也无 report：400 语义。
	if _, _, err := env.svc.CreatePreview(context.Background(), env.userID, hair.CreatePreviewInput{
		StyleID: uuid.NewString(),
	}); !errors.Is(err, hair.ErrFaceMissing) {
		t.Fatalf("no face err = %v, want ErrFaceMissing", err)
	}

	// 他人 media_id：越权与不存在不可区分，一律 NotFound。
	other := insertUser(t, env.pool)
	if _, _, err := env.svc.CreatePreview(context.Background(), other, hair.CreatePreviewInput{
		MediaAssetID: faceID, StyleID: uuid.NewString(),
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user media err = %v, want ErrNotFound", err)
	}
}

// ---- fakes ----

type memoryObjects struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryObjects() *memoryObjects {
	return &memoryObjects{data: map[string][]byte{}}
}

func (s *memoryObjects) Save(_ context.Context, key string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = data
	return key, nil
}

func (s *memoryObjects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("object not found: " + key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memoryObjects) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

type objectSourceLoader struct{ objects *memoryObjects }

func (l objectSourceLoader) Load(_ context.Context, work hair.PreviewWork) (hair.SourceImage, error) {
	reader, err := l.objects.Open(context.Background(), work.SourceObjectKey)
	if err != nil {
		return hair.SourceImage{}, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		return hair.SourceImage{}, err
	}
	return hair.SourceImage{MIMEType: work.SourceMIMEType, Data: data}, nil
}

// fakeGenerator 替代 hair_edit 能力路由：输出确定性 JPEG，且与生产同形地
// 落 provider_invocations 台账行（媒体资产外键引用它）。
type fakeGenerator struct {
	pool        *pgxpool.Pool
	userID      string
	operationID string
	taskID      string
	data        []byte
	err         error
}

func (g *fakeGenerator) Generate(ctx context.Context, input hair.GenerateInput) (hair.GenerateOutput, error) {
	if g.err != nil {
		return hair.GenerateOutput{}, g.err
	}
	data := g.data
	if len(data) == 0 {
		img := image.NewRGBA(image.Rect(0, 0, 640, 640))
		for x := 0; x < 640; x += 8 {
			for y := 0; y < 640; y += 8 {
				img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 200, A: 255})
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return hair.GenerateOutput{}, err
		}
		data = buf.Bytes()
	}
	sum := sha256.Sum256([]byte(input.Prompt))
	var invocationID string
	err := g.pool.QueryRow(ctx, `
		INSERT INTO provider_invocations(
			user_id, operation_id, task_id, attempt_no, capability,
			routing_config_version, provider_key, model_key, protocol, request_hash, status
		) VALUES ($1::uuid,$2::uuid,$3::uuid,0,'hair_edit','e2e','e2e','e2e','dashscope_wan',$4,'succeeded')
		RETURNING id::text`, g.userID, g.operationID, g.taskID, hex.EncodeToString(sum[:])).Scan(&invocationID)
	if err != nil {
		return hair.GenerateOutput{}, err
	}
	return hair.GenerateOutput{Data: data, MIMEType: "image/jpeg", InvocationID: invocationID}, nil
}

type renderingNormalizer struct{}

func (renderingNormalizer) Normalize(data []byte, declaredMIME string) (hair.NormalizedImage, error) {
	normalized, err := rendering.NewJPEGNormalizer().Normalize(data, declaredMIME)
	if err != nil {
		return hair.NormalizedImage{}, err
	}
	return hair.NormalizedImage{
		Data: normalized.Data, MIMEType: normalized.MIMEType, SHA256: normalized.SHA256,
		ByteSize: normalized.ByteSize, Width: normalized.Width, Height: normalized.Height,
	}, nil
}

type storeProgress struct{ store *postgres.Store }

func (p storeProgress) Set(ctx context.Context, operationID string, progressBPS int, stageCode string) error {
	return p.store.SetOperationProgress(ctx, operationID, progressBPS, stageCode)
}

type testSigner struct{}

func (testSigner) SignedURL(_ context.Context, objectKey string) (string, time.Time, error) {
	return "https://test.invalid/signed/" + objectKey, time.Now().Add(15 * time.Minute).UTC(), nil
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x += 40 {
		for y := 0; y < height; y += 40 {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}
