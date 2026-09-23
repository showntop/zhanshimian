package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/httpapi"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/service/operation"
	"github.com/zhanshimian/server/internal/storage"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestFoundationTraceSurvivesLeaseRecovery(t *testing.T) {
	env := newFoundationEnvironment(t)
	ctx := context.Background()

	intent := env.CreateUploadIntent("user-a")
	first, created, err := env.store.CompleteUploadIntent(ctx, domain.CompleteUploadIntent{
		UserID: env.user("user-a"), IntentID: intent.ID, Metadata: metaFor(intent),
	})
	if err != nil || !created {
		t.Fatalf("complete upload: created=%v err=%v", created, err)
	}
	replay, created, err := env.store.CompleteUploadIntent(ctx, domain.CompleteUploadIntent{
		UserID: env.user("user-a"), IntentID: intent.ID, Metadata: metaFor(intent),
	})
	if err != nil || created {
		t.Fatalf("replay complete: created=%v err=%v", created, err)
	}
	if first.ID != replay.ID {
		t.Fatalf("replay asset %s != %s", replay.ID, first.ID)
	}

	op := env.CreateOperation("user-a", "assessment")
	if _, err := env.store.GetOperation(ctx, env.user("user-a"), op.ID); err != nil {
		t.Fatalf("owner GetOperation: %v", err)
	}
	if _, err := env.store.GetOperation(ctx, env.user("user-b"), op.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user GetOperation error = %v, want ErrNotFound", err)
	}

	task := env.EnqueueTask(op, "assessment", 7)
	oldLease := env.Claim("worker-a")
	env.Expire(oldLease)
	newLease := env.Claim("worker-b")
	if newLease.LeaseToken == oldLease.LeaseToken || newLease.LeaseOwner != "worker-b" {
		t.Fatalf("reclaim lease = %#v old=%#v", newLease, oldLease)
	}
	invocation := env.RecordInvocation(newLease)

	before := env.snapshot(op.ID, task.ID)
	if err := env.Commit(oldLease, 7); !errors.Is(err, repository.ErrLeaseLost) {
		t.Fatalf("stale commit err = %v, want ErrLeaseLost", err)
	}
	if after := env.snapshot(op.ID, task.ID); after != before {
		t.Fatalf("stale commit changed rows\nbefore %#v\nafter  %#v", before, after)
	}

	if err := env.Commit(newLease, 7); err != nil {
		t.Fatalf("task lease was not committed: %v", err)
	}
	trace := env.Trace(op.ID)
	if trace.Task.ID != task.ID {
		t.Fatalf("trace task = %s, want %s", trace.Task.ID, task.ID)
	}
	if trace.Invocation.ID != invocation.ID {
		t.Fatalf("trace invocation = %s, want %s", trace.Invocation.ID, invocation.ID)
	}
	if newLease.Attempt != trace.Invocation.AttemptNo {
		t.Fatalf("attempt = %d, want %d", trace.Invocation.AttemptNo, newLease.Attempt)
	}
	if trace.Operation.Status != domain.OperationSucceeded {
		t.Fatalf("operation remains running: status=%s", trace.Operation.Status)
	}
	if trace.Operation.ProgressBPS != 10000 {
		t.Fatalf("progress_bps = %d, want 10000", trace.Operation.ProgressBPS)
	}
	if trace.Task.Status != domain.TaskSucceeded {
		t.Fatalf("task status = %s, want succeeded", trace.Task.Status)
	}
}

func TestFoundationTraceRefundsFailedOperation(t *testing.T) {
	env := newFoundationEnvironment(t)
	ctx := context.Background()
	op := env.CreateOperation("user-a", "assessment")
	env.seedWallet("user-a", 1)

	reserved, err := env.billing.Reserve(ctx, env.user("user-a"), op.ID, domain.ProductRenderPublication, 1)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if reserved.Status != domain.BillingReserved {
		t.Fatalf("reservation status = %s", reserved.Status)
	}
	if got := env.walletCredits("user-a"); got != 0 {
		t.Fatalf("credits after reserve = %d, want 0", got)
	}

	env.markOperationTerminal(op, domain.OperationFailed)
	if err := env.billing.Refund(ctx, env.user("user-a"), op.ID); err != nil {
		t.Fatalf("first Refund: %v", err)
	}
	if err := env.billing.Refund(ctx, env.user("user-a"), op.ID); err != nil {
		t.Fatalf("second Refund: %v", err)
	}
	if got := env.walletCredits("user-a"); got != 1 {
		t.Fatalf("credits after refund-once = %d, want 1", got)
	}

	settledOp := env.CreateOperation("user-a", "assessment")
	env.seedWallet("user-a", 1)
	if _, err := env.billing.Reserve(ctx, env.user("user-a"), settledOp.ID, domain.ProductAssessment, 1); err != nil {
		t.Fatalf("settle-path Reserve: %v", err)
	}
	env.markOperationSucceeded(settledOp, "report", uuid.NewString())
	if err := env.billing.Settle(ctx, env.user("user-a"), settledOp.ID, nil); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if err := env.billing.Refund(ctx, env.user("user-a"), settledOp.ID); !errors.Is(err, billing.ErrAlreadySettled) {
		t.Fatalf("Refund after settle error = %v, want ErrAlreadySettled", err)
	}
}

func TestFoundationTraceHTTPOwnershipAndIdempotency(t *testing.T) {
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	objects := foundationObjects{
		ObjectStorage: mustLocalStore(t),
		ObjectStore:   newFoundationObjectStore(),
	}
	frepo := &foundationRepo{Store: store, pool: pool}
	deps := httpapi.Dependencies{
		Idempotency: frepo,
		Operations:  operation.New(store),
		Account: account.New(frepo, frepo, frepo, frepo, frepo, nil, nil, nil, nil,
			foundationAvatarResolver{}, nil, account.Config{SessionTTL: time.Hour}),
	}
	deps.Media = media.New(frepo, objects, 10<<20, time.Hour)
	server := httptest.NewServer(httpapi.New(deps, discardLogger(), true, httpapi.RuntimeInfo{}))
	t.Cleanup(server.Close)

	tokenA := devLogin(t, server.URL, "user-a")
	tokenB := devLogin(t, server.URL, "user-b")
	userA := userFromToken(t, store, tokenA)

	opID := insertHTTPOperation(t, pool, userA)

	createBody := map[string]any{
		"purpose": "face", "mime_type": "image/jpeg", "byte_size": 20, "sha256": strings.Repeat("b", 64),
	}
	first := doHTTPJSON(t, server.URL, tokenA, http.MethodPost, "/v1/media/upload-intents", createBody, "foundation-intent-1")
	if first.status != http.StatusCreated {
		t.Fatalf("create intent status = %d body=%s", first.status, first.body)
	}
	intentID := jsonString(t, first.body, "data", "id")
	if intentID == "" {
		t.Fatalf("missing intent id: %s", first.body)
	}

	complete := doHTTPJSON(t, server.URL, tokenA, http.MethodPost, "/v1/media/upload-intents/"+intentID+"/complete", map[string]any{}, "foundation-complete-1")
	if complete.status != http.StatusCreated {
		t.Fatalf("complete status = %d body=%s", complete.status, complete.body)
	}

	owner := doHTTPJSON(t, server.URL, tokenA, http.MethodGet, "/v1/operations/"+opID, nil, "")
	if owner.status != http.StatusOK {
		t.Fatalf("owner GET operation status = %d body=%s", owner.status, owner.body)
	}
	if jsonString(t, owner.body, "data", "id") != opID {
		t.Fatalf("owner operation id = %s", owner.body)
	}

	other := doHTTPJSON(t, server.URL, tokenB, http.MethodGet, "/v1/operations/"+opID, nil, "")
	if other.status != http.StatusNotFound {
		t.Fatalf("cross-user GET operation status = %d body=%s", other.status, other.body)
	}

	task404 := doHTTPJSON(t, server.URL, tokenA, http.MethodGet, "/v1/tasks/"+opID, nil, "")
	if task404.status != http.StatusNotFound {
		t.Fatalf("GET /v1/tasks/{id} status = %d body=%s, want 404", task404.status, task404.body)
	}

	replay := doHTTPJSON(t, server.URL, tokenA, http.MethodPost, "/v1/media/upload-intents", createBody, "foundation-intent-1")
	if replay.status != http.StatusCreated {
		t.Fatalf("replay create status = %d body=%s", replay.status, replay.body)
	}
	if jsonString(t, replay.body, "data", "id") != intentID {
		t.Fatalf("idempotent create id = %s, want %s body=%s", jsonString(t, replay.body, "data", "id"), intentID, replay.body)
	}
}

type foundationEnv struct {
	t        *testing.T
	pool     *pgxpool.Pool
	store    *postgres.Store
	billing  *billing.Service
	recorder *ai.InvocationRecorder
	users    map[string]string
}

func newFoundationEnvironment(t *testing.T) *foundationEnv {
	t.Helper()
	pool := testutil.NewPostgres(t)
	store := postgres.New(pool)
	return &foundationEnv{
		t:        t,
		pool:     pool,
		store:    store,
		billing:  billing.New(store),
		recorder: ai.NewInvocationRecorder(store, nil),
		users:    map[string]string{},
	}
}

func (e *foundationEnv) user(label string) string {
	e.t.Helper()
	if id, ok := e.users[label]; ok {
		return id
	}
	var id string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES($1) RETURNING id::text`, label).Scan(&id); err != nil {
		e.t.Fatalf("insert user %s: %v", label, err)
	}
	e.users[label] = id
	return id
}

func (e *foundationEnv) CreateUploadIntent(label string) domain.UploadIntent {
	e.t.Helper()
	userID := e.user(label)
	intent, err := e.store.CreateUploadIntent(context.Background(), domain.CreateUploadIntent{
		UserID:    userID,
		Purpose:   domain.MediaPurposeFace,
		MIMEType:  "image/jpeg",
		ByteSize:  20,
		SHA256:    strings.Repeat("b", 64),
		ObjectKey: fmt.Sprintf("users/%s/uploads/%s", userID, uuid.NewString()),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	})
	if err != nil {
		e.t.Fatalf("CreateUploadIntent: %v", err)
	}
	return intent
}

func (e *foundationEnv) CreateOperation(label, kind string) domain.Operation {
	e.t.Helper()
	if kind == "" {
		kind = "assessment"
	}
	userID := e.user(label)
	var id string
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status, stage_code)
		VALUES ($1::uuid, $2, 'assessment', $3::uuid, 'accepted', 'queued')
		RETURNING id::text`, userID, kind, uuid.NewString()).Scan(&id)
	if err != nil {
		e.t.Fatalf("CreateOperation: %v", err)
	}
	op, err := e.store.GetOperation(context.Background(), userID, id)
	if err != nil {
		e.t.Fatalf("reload operation: %v", err)
	}
	return op
}

func (e *foundationEnv) EnqueueTask(op domain.Operation, taskType domain.TaskType, generation int64) domain.Task {
	e.t.Helper()
	var task domain.Task
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO tasks (
			user_id, operation_id, type, subject_type, subject_id, subject_generation,
			payload_version, payload, dedupe_key, status, priority, max_attempts, stage_code
		) VALUES (
			$1::uuid, $2::uuid, $3, 'assessment', $4::uuid, $5,
			1, '{}'::jsonb, $6, 'queued', 0, 3, 'queued'
		) RETURNING id::text, user_id::text, operation_id::text, type, subject_generation, status, attempt`,
		op.UserID, op.ID, taskType, uuid.NewString(), generation, uuid.NewString(),
	).Scan(&task.ID, &task.UserID, &task.OperationID, &task.Type, &task.SubjectGeneration, &task.Status, &task.Attempt)
	if err != nil {
		e.t.Fatalf("EnqueueTask: %v", err)
	}
	return task
}

func (e *foundationEnv) Claim(owner string) domain.TaskLease {
	e.t.Helper()
	lease, ok, err := e.store.Claim(context.Background(), owner, 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		e.t.Fatalf("Claim %s: ok=%v err=%v", owner, ok, err)
	}
	return lease
}

func (e *foundationEnv) Expire(lease domain.TaskLease) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE tasks SET lease_expires_at=now() - interval '1 second' WHERE id=$1::uuid`, lease.ID); err != nil {
		e.t.Fatalf("Expire: %v", err)
	}
}

func (e *foundationEnv) RecordInvocation(lease domain.TaskLease) domain.ProviderInvocation {
	e.t.Helper()
	_, meta, err := e.recorder.RecordInvocation(context.Background(), domain.StartInvocation{
		UserID:               lease.UserID,
		OperationID:          lease.OperationID,
		TaskID:               lease.ID,
		AttemptNo:            lease.Attempt,
		Capability:           "photo_quality_check",
		RoutingConfigVersion: "foundation-v1",
		ProviderKey:          "primary",
		ModelKey:             "vision-main",
		Protocol:             "openai_images",
		RequestHash:          fmt.Sprintf("%x", sha256.Sum256([]byte("foundation canonical request"))),
		InputImages:          1,
	}, func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{ProviderRequestID: "foundation-req"}, nil
	})
	if err != nil {
		e.t.Fatalf("RecordInvocation: %v", err)
	}
	return domain.ProviderInvocation{ID: meta.InvocationID, AttemptNo: lease.Attempt}
}

func (e *foundationEnv) Commit(lease domain.TaskLease, generation int64) error {
	return e.store.CommitLeasedTask(context.Background(), lease, generation)
}

func (e *foundationEnv) Trace(operationID string) foundationTrace {
	e.t.Helper()
	var userID string
	if err := e.pool.QueryRow(context.Background(), `SELECT user_id::text FROM operations WHERE id=$1::uuid`, operationID).Scan(&userID); err != nil {
		e.t.Fatalf("Trace user: %v", err)
	}
	op, err := e.store.GetOperation(context.Background(), userID, operationID)
	if err != nil {
		e.t.Fatalf("Trace operation: %v", err)
	}
	var task domain.Task
	if err := e.pool.QueryRow(context.Background(), `
		SELECT id::text, status FROM tasks WHERE operation_id=$1::uuid`, operationID).
		Scan(&task.ID, &task.Status); err != nil {
		e.t.Fatalf("Trace task: %v", err)
	}
	var invocation domain.ProviderInvocation
	if err := e.pool.QueryRow(context.Background(), `
		SELECT id::text, attempt_no FROM provider_invocations WHERE operation_id=$1::uuid`, operationID).
		Scan(&invocation.ID, &invocation.AttemptNo); err != nil {
		e.t.Fatalf("Trace invocation: %v", err)
	}
	return foundationTrace{Operation: op, Task: task, Invocation: invocation}
}

func (e *foundationEnv) seedWallet(label string, credits int) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO billing_wallets(user_id, credits) VALUES($1::uuid, $2)
		ON CONFLICT (user_id) DO UPDATE SET credits=EXCLUDED.credits`, e.user(label), credits); err != nil {
		e.t.Fatalf("seed wallet: %v", err)
	}
}

func (e *foundationEnv) walletCredits(label string) int {
	e.t.Helper()
	var credits int
	if err := e.pool.QueryRow(context.Background(), `SELECT credits FROM billing_wallets WHERE user_id=$1::uuid`, e.user(label)).Scan(&credits); err != nil {
		e.t.Fatalf("wallet credits: %v", err)
	}
	return credits
}

func (e *foundationEnv) markOperationTerminal(op domain.Operation, status domain.OperationStatus) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		UPDATE operations
		SET status=$3, trace_id=COALESCE(trace_id, $4), finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`,
		op.UserID, op.ID, status, "foundation-fail-trace"); err != nil {
		e.t.Fatalf("mark operation terminal: %v", err)
	}
}

func (e *foundationEnv) markOperationSucceeded(op domain.Operation, resultType, resultID string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		UPDATE operations
		SET status='succeeded', result_type=$3, result_id=$4::uuid, finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`,
		op.UserID, op.ID, resultType, resultID); err != nil {
		e.t.Fatalf("mark operation succeeded: %v", err)
	}
}

type foundationTrace struct {
	Operation  domain.Operation
	Task       domain.Task
	Invocation domain.ProviderInvocation
}

type foundationSnapshot struct {
	TaskStatus   string
	LeaseToken   string
	LeaseOwner   string
	TaskUpdated  time.Time
	OpStatus     string
	OpProgress   int
	OpVersion    int
	OpUpdated    time.Time
	Invocations  int
	Reservations int
	Assets       int
}

func (e *foundationEnv) snapshot(operationID, taskID string) foundationSnapshot {
	e.t.Helper()
	var snap foundationSnapshot
	var token, owner *string
	if err := e.pool.QueryRow(context.Background(), `
		SELECT status, lease_token::text, lease_owner, updated_at FROM tasks WHERE id=$1::uuid`, taskID).
		Scan(&snap.TaskStatus, &token, &owner, &snap.TaskUpdated); err != nil {
		e.t.Fatalf("snapshot task: %v", err)
	}
	if token != nil {
		snap.LeaseToken = *token
	}
	if owner != nil {
		snap.LeaseOwner = *owner
	}
	if err := e.pool.QueryRow(context.Background(), `
		SELECT status, progress_bps, version, updated_at FROM operations WHERE id=$1::uuid`, operationID).
		Scan(&snap.OpStatus, &snap.OpProgress, &snap.OpVersion, &snap.OpUpdated); err != nil {
		e.t.Fatalf("snapshot operation: %v", err)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM provider_invocations WHERE operation_id=$1::uuid`, operationID).Scan(&snap.Invocations); err != nil {
		e.t.Fatalf("snapshot invocations: %v", err)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM billing_reservations WHERE operation_id=$1::uuid`, operationID).Scan(&snap.Reservations); err != nil {
		e.t.Fatalf("snapshot reservations: %v", err)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM media_assets WHERE user_id=(SELECT user_id FROM operations WHERE id=$1::uuid)`, operationID).Scan(&snap.Assets); err != nil {
		e.t.Fatalf("snapshot assets: %v", err)
	}
	return snap
}

func metaFor(intent domain.UploadIntent) domain.ObjectMetadata {
	return domain.ObjectMetadata{
		ObjectKey: intent.ObjectKey, MIMEType: intent.MIMEType,
		ByteSize: intent.ByteSize, SHA256: intent.SHA256,
	}
}

type foundationRepo struct {
	*postgres.Store
	pool *pgxpool.Pool
}

func (r *foundationRepo) CreateDevUser(ctx context.Context, nickname string) (domain.User, error) {
	var user domain.User
	err := r.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES($1) RETURNING id::text, nickname, created_at`, nickname).
		Scan(&user.ID, &user.Nickname, &user.CreatedAt)
	return user, err
}

type foundationObjects struct {
	storage.ObjectStorage
	media.ObjectStore
}

// Open 二义消解：上传完成的嗅探读走 fake（测试不落真实字节），
// 其余读取仍由 ObjectStorage 承担。
func (f foundationObjects) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return f.ObjectStore.Open(ctx, key)
}

type foundationObjectStore struct {
	objects map[string]domain.ObjectMetadata
}

func newFoundationObjectStore() *foundationObjectStore {
	return &foundationObjectStore{objects: map[string]domain.ObjectMetadata{}}
}

func (s *foundationObjectStore) PresignUpload(_ context.Context, intent domain.UploadIntent, ttl time.Duration) (domain.UploadGrant, error) {
	s.objects[intent.ObjectKey] = domain.ObjectMetadata{
		ObjectKey: intent.ObjectKey, MIMEType: intent.MIMEType,
		ByteSize: intent.ByteSize, SHA256: intent.SHA256,
	}
	return domain.UploadGrant{
		Method: "PUT",
		URL:    "https://private-cos.example/foundation-put",
		Headers: map[string]string{
			"Content-Type": intent.MIMEType,
		},
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (s *foundationObjectStore) HeadObject(_ context.Context, key string) (domain.ObjectMetadata, error) {
	meta, ok := s.objects[key]
	if !ok {
		return domain.ObjectMetadata{}, storage.ErrObjectNotFound
	}
	return meta, nil
}

func (s *foundationObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	meta, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	if meta.MIMEType == "image/png" {
		return io.NopCloser(bytes.NewReader([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1})), nil
	}
	return io.NopCloser(bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xE0, 1})), nil
}

func mustLocalStore(t *testing.T) storage.ObjectStorage {
	t.Helper()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return local
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func devLogin(t *testing.T, baseURL, nickname string) string {
	t.Helper()
	res := doHTTPJSON(t, baseURL, "", http.MethodPost, "/v1/auth/dev", map[string]any{"nickname": nickname}, "")
	if res.status != http.StatusCreated {
		t.Fatalf("dev login %s status = %d body=%s", nickname, res.status, res.body)
	}
	token := jsonString(t, res.body, "data", "token")
	if token == "" {
		t.Fatalf("dev login missing token: %s", res.body)
	}
	return token
}

func userFromToken(t *testing.T, store *postgres.Store, token string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	user, err := store.UserByTokenDigest(context.Background(), digest[:])
	if err != nil {
		t.Fatalf("UserByTokenDigest: %v", err)
	}
	return user.ID
}

func insertHTTPOperation(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid, 'assessment', 'assessment', $2::uuid, 'accepted')
		RETURNING id::text`, userID, uuid.NewString()).Scan(&id)
	if err != nil {
		t.Fatalf("seed http operation: %v", err)
	}
	return id
}

type httpResult struct {
	status int
	body   string
}

func doHTTPJSON(t *testing.T, baseURL, token, method, path string, body any, idempotencyKey string) httpResult {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return httpResult{status: res.StatusCode, body: string(raw)}
}

func jsonString(t *testing.T, raw string, keys ...string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("json: %v body=%s", err, raw)
	}
	current := value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[key]
		if !ok {
			return ""
		}
	}
	text, _ := current.(string)
	return text
}

// foundationAvatarResolver 集成测试头像解析：key 原样返回。
type foundationAvatarResolver struct{}

func (foundationAvatarResolver) ResolveAssetURL(objectKey string) string { return objectKey }
