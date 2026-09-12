package assessment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/testutil"
)

// e2e_test.go drives the whole assessment chain on real PostgreSQL with the
// real service, repository, handler and provider adapters. Only the object
// store (in-memory) and the AI runtime (deterministic stub that still lands
// every call in the invocation ledger) are replaced, so the technical gate,
// evidence gate and publish transaction run exactly as in production. The
// HTTP envelope itself is pinned by the httpapi contract tests.

func TestAssessmentToPublishedReport(t *testing.T) {
	env := newAssessmentE2E(t)
	slots := env.uploadThreeValidPhotos()
	accepted := env.postAssessment(slots)
	if accepted.Operation.Status != domain.OperationAccepted {
		t.Fatalf("operation status = %s, want accepted", accepted.Operation.Status)
	}

	env.runTask()
	op := env.getOperation(accepted.Operation.ID)
	if op.Status != domain.OperationSucceeded {
		t.Fatalf("operation status = %s, want succeeded", op.Status)
	}
	if op.ResultType != "report" || op.ResultID == "" {
		t.Fatalf("operation result = %s/%s, want report", op.ResultType, op.ResultID)
	}

	report := env.getReport(op.ResultID)
	if report.SourceMedia.Face.ItemID == "" || report.SourceMedia.Side.ItemID == "" || report.SourceMedia.Body.ItemID == "" {
		t.Fatalf("report source media incomplete: %#v", report.SourceMedia)
	}
	if len(report.Findings) < 3 {
		t.Fatalf("findings = %d, want >= 3", len(report.Findings))
	}
	for _, finding := range report.Findings {
		if finding.SourcePhoto.ItemID == "" || finding.VisibleObservation == "" || finding.Recommendation == "" {
			t.Fatalf("finding lacks evidence: %#v", finding)
		}
	}
	if _, err := env.svc.GetReport(context.Background(), env.userID, op.ResultID); err != nil {
		t.Fatalf("published report is not readable: %v", err)
	}
	if env.reportCount() != 1 {
		t.Fatalf("report count = %d, want 1", env.reportCount())
	}
	if current := env.currentReportID(); current == nil || *current != op.ResultID {
		t.Fatalf("current report pointer = %v, want %s", current, op.ResultID)
	}
}

func TestRejectedPhotosNeverCreateReadableReport(t *testing.T) {
	env := newAssessmentE2E(t)
	slots := env.uploadThreeValidPhotos()
	env.runtime.contentReject = true
	accepted := env.postAssessment(slots)

	env.runTask()
	op := env.getOperation(accepted.Operation.ID)
	if op.Status != domain.OperationFailed {
		t.Fatalf("operation status = %s, want failed", op.Status)
	}
	if op.ErrorCode != "photo_content_rejected" {
		t.Fatalf("error code = %q, want photo_content_rejected", op.ErrorCode)
	}
	if op.ResultID != "" {
		t.Fatalf("failed operation carries result %q", op.ResultID)
	}
	if env.reportCount() != 0 {
		t.Fatalf("rejected run published %d reports", env.reportCount())
	}
	if env.currentReportID() != nil {
		t.Fatal("rejected run moved the current report pointer")
	}
}

// ---- environment ----

type e2eEnv struct {
	t       *testing.T
	pool    *pgxpool.Pool
	store   *postgres.Store
	storage *e2eObjectStore
	runtime *e2eRuntime
	svc     *Service
	handler *Handler
	userID  string
}

func newAssessmentE2E(t *testing.T) *e2eEnv {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	userID := e2eInsertUser(t, pool)
	storage := newE2EObjectStore()
	runtime := &e2eRuntime{pool: pool, userID: userID}
	providers := ai.NewAssessmentProviders(runtime)
	definition := taskrunner.Definition{
		Type:           TaskTypeAssessment,
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		Concurrency:    1,
	}
	svc := NewService(store, e2eAssetReader{pool: pool}, e2eProfiles{}, e2eMedia{}, definition)
	handler := NewHandler(HandlerDeps{
		Repo:         store,
		Images:       e2eImages{storage: storage},
		Progress:     e2eProgress{pool: pool},
		Technical:    TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 8192, MaxPixels: 40_000_000},
		PhotoContent: providers.PhotoContent,
		Identity:     providers.Identity,
		Analyzer:     providers.Analyzer,
		Evidence:     providers.Evidence,
		Policy:       Policy{},
	})
	return &e2eEnv{t: t, pool: pool, store: store, storage: storage, runtime: runtime, svc: svc, handler: handler, userID: userID}
}

func (e *e2eEnv) uploadThreeValidPhotos() domain.PhotoSlots {
	e.t.Helper()
	return domain.PhotoSlots{
		FaceAssetID: e.insertPhoto("face"),
		SideAssetID: e.insertPhoto("side"),
		BodyAssetID: e.insertPhoto("body"),
	}
}

func (e *e2eEnv) insertPhoto(purpose string) string {
	e.t.Helper()
	key := "users/" + e.userID + "/uploads/" + uuid.NewString() + ".jpg"
	data := e2eJPEG(e.t, 600, 800)
	if _, err := e.storage.Save(context.Background(), key, bytes.NewReader(data)); err != nil {
		e.t.Fatalf("save object: %v", err)
	}
	var id string
	sum := sha256.Sum256(data)
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, width, height, state, display_kind)
		VALUES ($1::uuid,'user_upload',$2,$3,$4,'image/jpeg',$5,600,800,'ready','original')
		RETURNING id::text`,
		e.userID, purpose, key, hex.EncodeToString(sum[:]), len(data),
	).Scan(&id)
	if err != nil {
		e.t.Fatalf("insert media %s: %v", purpose, err)
	}
	return id
}

func (e *e2eEnv) postAssessment(slots domain.PhotoSlots) CreateResult {
	e.t.Helper()
	result, err := e.svc.Create(context.Background(), CreateCommand{UserID: e.userID, Slots: slots})
	if err != nil {
		e.t.Fatalf("create assessment: %v", err)
	}
	e.runtime.operationID = result.Operation.ID
	e.runtime.taskID = result.Task.ID
	return result
}

// runTask claims the queued assessment task and drives Execute + Commit the
// way the task runner loop does, without bypassing the gates or the lease
// CAS commit.
func (e *e2eEnv) runTask() {
	e.t.Helper()
	lease, ok, err := e.store.Claim(context.Background(), "e2e-worker", 30*time.Second, []domain.TaskType{TaskTypeAssessment})
	if err != nil || !ok {
		e.t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	result, err := e.handler.Execute(context.Background(), lease)
	if err != nil {
		e.t.Fatalf("execute: %v", err)
	}
	outcome, err := e.handler.Commit(context.Background(), lease, result)
	if err != nil || outcome != domain.CommitApplied {
		e.t.Fatalf("commit: outcome=%s err=%v", outcome, err)
	}
}

func (e *e2eEnv) getOperation(id string) domain.Operation {
	e.t.Helper()
	op, err := e.store.GetOperation(context.Background(), e.userID, id)
	if err != nil {
		e.t.Fatalf("get operation: %v", err)
	}
	return op
}

func (e *e2eEnv) getReport(id string) ReportView {
	e.t.Helper()
	view, err := e.svc.GetReport(context.Background(), e.userID, id)
	if err != nil {
		e.t.Fatalf("get report: %v", err)
	}
	return view
}

func (e *e2eEnv) reportCount() int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM reports WHERE user_id=$1::uuid`, e.userID).Scan(&n); err != nil {
		e.t.Fatalf("count reports: %v", err)
	}
	return n
}

func (e *e2eEnv) currentReportID() *string {
	e.t.Helper()
	var id *string
	err := e.pool.QueryRow(context.Background(),
		`SELECT current_report_id::text FROM user_profiles WHERE user_id=$1::uuid`, e.userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		e.t.Fatalf("current report pointer: %v", err)
	}
	return id
}

// ---- deterministic AI runtime stub ----

type e2eRuntime struct {
	pool          *pgxpool.Pool
	userID        string
	operationID   string
	taskID        string
	contentReject bool
}

func (r *e2eRuntime) Structured(ctx context.Context, req ai.StructuredRequest) (ai.StructuredResult, error) {
	invocationID, err := r.startInvocation(ctx, req)
	if err != nil {
		return ai.StructuredResult{}, err
	}
	var payload []byte
	switch req.Capability {
	case "photo_quality_check":
		payload = []byte(`{"photos":[` +
			`{"role":"face","decision":"reject","reason_code":"no_person"},` +
			`{"role":"side","decision":"reject","reason_code":"no_person"},` +
			`{"role":"body","decision":"reject","reason_code":"no_person"}]}`)
		if !r.contentReject {
			payload = []byte(`{"photos":[` +
				`{"role":"face","decision":"pass","reason_code":""},` +
				`{"role":"side","decision":"pass","reason_code":""},` +
				`{"role":"body","decision":"pass","reason_code":""}]}`)
		}
	case "photo_identity_consistency":
		payload = []byte(`{"decision":"pass","confidence":0.97}`)
	case "appearance_analysis":
		payload = []byte(e2eDraftJSON)
	case "report_evidence_verification":
		payload = []byte(`{"findings":[` +
			`{"key":"f1","supported":true,"confidence":0.98,"reason_code":""},` +
			`{"key":"f2","supported":true,"confidence":0.97,"reason_code":""},` +
			`{"key":"f3","supported":true,"confidence":0.96,"reason_code":""}]}`)
	default:
		return ai.StructuredResult{}, errors.New("e2e runtime: unexpected capability " + req.Capability)
	}
	if req.Validate != nil {
		if err := req.Validate(payload); err != nil {
			return ai.StructuredResult{}, err
		}
	}
	return ai.StructuredResult{
		JSON: payload,
		Meta: ai.InvocationMeta{InvocationID: invocationID},
	}, nil
}

func (r *e2eRuntime) startInvocation(ctx context.Context, req ai.StructuredRequest) (string, error) {
	sum := sha256.Sum256([]byte(req.Capability + "|" + req.Prompt))
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO provider_invocations(
			user_id, operation_id, task_id, attempt_no, capability,
			routing_config_version, provider_key, model_key, protocol, request_hash, status
		) VALUES ($1::uuid,$2::uuid,$3::uuid,0,$4,'e2e','e2e','e2e','structured',$5,'succeeded')
		RETURNING id::text`,
		r.userID, r.operationID, r.taskID, req.Capability, hex.EncodeToString(sum[:]),
	).Scan(&id)
	return id, err
}

const e2eDraftJSON = `{
  "impression_tags": ["利落", "干净"],
  "priority_title": "先整理额前碎发",
  "priority_copy": "额前碎发落到眉毛上方，先固定发根再整体定形。",
  "findings": [
    {"key": "f1", "category": "hair", "label": "额前碎发", "visible_observation": "额前碎发落到眉毛上方", "recommendation": "向后梳理并固定", "priority": 1, "position": 1, "source_role": "face", "anchor": {"x": 0.1, "y": 0.1, "w": 0.2, "h": 0.3}, "confidence": 0.98},
    {"key": "f2", "category": "outfit", "label": "肩线偏塌", "visible_observation": "上衣肩线低于自然肩点", "recommendation": "换成合肩线的上装", "priority": 2, "position": 2, "source_role": "body", "anchor": {"x": 0.3, "y": 0.3, "w": 0.4, "h": 0.2}, "confidence": 0.97},
    {"key": "f3", "category": "color", "label": "整体色偏灰", "visible_observation": "上装与背景同为灰色系", "recommendation": "用亮色内搭拉开层次", "priority": 3, "position": 3, "source_role": "side", "anchor": {"x": 0.6, "y": 0.5, "w": 0.3, "h": 0.3}, "confidence": 0.96}
  ]
}`

// ---- test adapters for the not-yet-wired production ports ----

func e2eInsertUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users(nickname) VALUES ('e2e') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func e2eJPEG(t *testing.T, width, height int) []byte {
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

type e2eObjectStore struct {
	data map[string][]byte
}

func newE2EObjectStore() *e2eObjectStore {
	return &e2eObjectStore{data: map[string][]byte{}}
}

func (s *e2eObjectStore) Save(_ context.Context, key string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s.data[key] = data
	return key, nil
}

func (s *e2eObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("e2e object store: object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *e2eObjectStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

type e2eAssetReader struct {
	pool *pgxpool.Pool
}

func (r e2eAssetReader) GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind, created_at
		FROM media_assets
		WHERE user_id=$1::uuid AND id=ANY($2::uuid[]) AND state='ready' AND deleted_at IS NULL`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := make([]domain.MediaAsset, 0, len(ids))
	for rows.Next() {
		var asset domain.MediaAsset
		if err := rows.Scan(&asset.ID, &asset.UserID, &asset.Origin, &asset.Purpose, &asset.ObjectKey,
			&asset.SHA256, &asset.MIMEType, &asset.ByteSize, &asset.State, &asset.DisplayKind, &asset.CreatedAt); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

type e2eProfiles struct{}

func (e2eProfiles) Snapshot(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"role":"daily"}`), nil
}

type e2eMedia struct{}

func (e2eMedia) Present(_ context.Context, asset domain.MediaAsset) (PresentedMedia, error) {
	return PresentedMedia{
		URL:          "https://e2e.invalid/" + asset.ObjectKey,
		URLExpiresAt: time.Now().Add(15 * time.Minute).UTC(),
		MIMEType:     asset.MIMEType,
		SourceKind:   "user_original",
		DisplayLabel: "原本",
	}, nil
}

type e2eImages struct {
	storage *e2eObjectStore
}

func (l e2eImages) Load(_ context.Context, items []domain.PhotoSetItem) ([]ImageInput, error) {
	images := make([]ImageInput, 0, len(items))
	for _, item := range items {
		data, ok := l.storage.data[item.Asset.ObjectKey]
		if !ok {
			return nil, errors.New("e2e image loader: object not found for " + item.Asset.ObjectKey)
		}
		images = append(images, ImageInput{Role: string(item.Role), MIMEType: item.Asset.MIMEType, Data: data})
	}
	return images, nil
}

type e2eProgress struct {
	pool *pgxpool.Pool
}

func (p e2eProgress) Set(_ context.Context, operationID string, progressBPS int, stageCode string) error {
	_, err := p.pool.Exec(context.Background(),
		`UPDATE operations SET status='running', progress_bps=$2, stage_code=$3 WHERE id=$1::uuid`,
		operationID, progressBPS, stageCode)
	return err
}
