package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

// isForeignKeyViolation reports a Postgres 23503 error raised because the row
// referenced by the named constraint disappeared underneath an in-flight
// worker job (for example a user data wipe cascading to analyses).
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

// ---- 身份、会话与资料 ----

// EnsureUserByIdentity binds one external identity to exactly one local user.
// The advisory lock serializes concurrent first-logins of the same identity so
// a duplicate user can never be created; nickname only seeds new users and
// never overwrites an existing one (users rename themselves in the profile).
func (s *Store) EnsureUserByIdentity(ctx context.Context, provider, identifier, nickname string) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "identity:"+provider+":"+identifier); err != nil {
		return domain.User{}, err
	}
	var user domain.User
	err = tx.QueryRow(ctx, `
		SELECT u.id::text,u.nickname FROM user_identities i
		JOIN users u ON u.id=i.user_id
		WHERE i.provider=$1 AND i.identifier=$2`, provider, identifier).Scan(&user.ID, &user.Nickname)
	if err == nil {
		return user, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, err
	}
	// wechat_miniapp identities keep the raw openid as users.open_id so rows
	// created before the identity table still match; other providers get a
	// synthesized, namespace-separated value (open_id stays NOT NULL UNIQUE).
	openID := identifier
	if provider != domain.ProviderWeChatMiniApp {
		openID = provider + ":" + identifier
	}
	var userID string
	err = tx.QueryRow(ctx, `INSERT INTO users(open_id,nickname) VALUES($1,$2) ON CONFLICT(open_id) DO NOTHING RETURNING id::text`, openID, nickname).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Pre-migration user row already carries this open_id.
		err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE open_id=$1`, openID).Scan(&userID)
	}
	if err != nil {
		return domain.User{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_identities(user_id,provider,identifier) VALUES($1,$2,$3) ON CONFLICT (provider,identifier) DO NOTHING`, userID, provider, identifier); err != nil {
		return domain.User{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT id::text,nickname FROM users WHERE id=$1::uuid`, userID).Scan(&user.ID, &user.Nickname); err != nil {
		return domain.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) CreateDevUser(ctx context.Context, nickname string) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `INSERT INTO users(open_id,nickname) VALUES($1,$2) RETURNING id::text,nickname`,
		"dev:"+newUUID(), nickname).Scan(&user.ID, &user.Nickname)
	return user, err
}

func (s *Store) ListIdentities(ctx context.Context, userID string) ([]domain.Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,provider,identifier,created_at FROM user_identities WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Identity, 0)
	for rows.Next() {
		var item domain.Identity
		if err := rows.Scan(&item.ID, &item.Provider, &item.Identifier, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSession(ctx context.Context, userID string, digest []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_sessions(user_id,token_digest,expires_at) VALUES($1,$2,$3)`, userID, digest, expiresAt)
	return err
}

// UserByTokenDigest resolves a bearer token and slides its expiry forward:
// sessions with less than 7 days left are extended to 30 days in the same
// round trip, so active users never see a surprise 401.
func (s *Store) UserByTokenDigest(ctx context.Context, digest []byte) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `
		UPDATE user_sessions SET expires_at=now() + interval '30 days'
		WHERE token_digest=$1 AND expires_at>now() AND expires_at<now() + interval '7 days'
		RETURNING user_id::text`, digest).Scan(&user.ID)
	if err == nil {
		err = s.pool.QueryRow(ctx, `SELECT nickname FROM users WHERE id=$1::uuid`, user.ID).Scan(&user.Nickname)
		return user, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT u.id::text,u.nickname FROM user_sessions ss
		JOIN users u ON u.id=ss.user_id
		WHERE ss.token_digest=$1 AND ss.expires_at>now()`, digest).Scan(&user.ID, &user.Nickname)
	return user, mapNotFound(err)
}

func (s *Store) DeleteSessionByTokenDigest(ctx context.Context, digest []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_sessions WHERE token_digest=$1`, digest)
	return err
}

func (s *Store) GetUserProfile(ctx context.Context, userID string) (domain.UserProfile, error) {
	var profile domain.UserProfile
	err := s.pool.QueryRow(ctx, `
		SELECT height_cm,role,budget,weight_kg::float8,bust_cm::float8,waist_cm::float8,hip_cm::float8,updated_at
		FROM user_profiles WHERE user_id=$1`, userID).
		Scan(&profile.HeightCM, &profile.Role, &profile.Budget, &profile.WeightKG, &profile.BustCM, &profile.WaistCM, &profile.HipCM, &profile.UpdatedAt)
	return profile, mapNotFound(err)
}

func (s *Store) SaveUserProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_profiles(user_id,height_cm,role,budget,weight_kg,bust_cm,waist_cm,hip_cm)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(user_id) DO UPDATE SET
			height_cm=EXCLUDED.height_cm,role=EXCLUDED.role,budget=EXCLUDED.budget,
			weight_kg=EXCLUDED.weight_kg,bust_cm=EXCLUDED.bust_cm,waist_cm=EXCLUDED.waist_cm,
			hip_cm=EXCLUDED.hip_cm,updated_at=now()
		RETURNING updated_at`, userID, profile.HeightCM, profile.Role, profile.Budget,
		profile.WeightKG, profile.BustCM, profile.WaistCM, profile.HipCM).
		Scan(&profile.UpdatedAt)
	return profile, err
}

// ---- 短信验证码 ----

func (s *Store) CreateSmsCode(ctx context.Context, phone string, digest []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sms_codes(phone,code_digest,expires_at) VALUES($1,$2,$3)`, phone, digest, expiresAt)
	return err
}

func (s *Store) LatestSmsCode(ctx context.Context, phone string) (domain.SmsCode, error) {
	var code domain.SmsCode
	err := s.pool.QueryRow(ctx, `
		SELECT id::text,phone,code_digest,expires_at,used_at,created_at
		FROM sms_codes WHERE phone=$1 ORDER BY created_at DESC LIMIT 1`, phone).
		Scan(&code.ID, &code.Phone, &code.Digest, &code.ExpiresAt, &code.UsedAt, &code.CreatedAt)
	return code, mapNotFound(err)
}

func (s *Store) CountSmsCodesSince(ctx context.Context, phone string, since time.Time) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sms_codes WHERE phone=$1 AND created_at>=$2`, phone, since).Scan(&count)
	return count, err
}

// ConsumeSmsCode burns a verification code exactly once; a second attempt
// reports ErrNotFound so the caller can reject replay.
func (s *Store) ConsumeSmsCode(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sms_codes SET used_at=now() WHERE id=$1::uuid AND used_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ---- 媒体 ----

func (s *Store) CreateMedia(ctx context.Context, userID, kind, storageKey, mime string, size int64) (domain.MediaAsset, error) {
	var item domain.MediaAsset
	err := s.pool.QueryRow(ctx, `
		INSERT INTO media_assets(user_id,kind,storage_key,mime_type,byte_size)
		VALUES($1,$2,$3,$4,$5) RETURNING id::text,kind,created_at`, userID, kind, storageKey, mime, size).
		Scan(&item.ID, &item.Kind, &item.CreatedAt)
	return item, err
}

func (s *Store) GetMediaAssets(ctx context.Context, ids []string) ([]domain.MediaAsset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text,kind,storage_key,mime_type,byte_size,created_at
		FROM media_assets WHERE id=ANY($1::uuid[]) AND deleted_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.MediaAsset, len(ids))
	for rows.Next() {
		var item domain.MediaAsset
		if err := rows.Scan(&item.ID, &item.Kind, &item.StorageKey, &item.MIMEType, &item.ByteSize, &item.CreatedAt); err != nil {
			return nil, err
		}
		byID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]domain.MediaAsset, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			return nil, repository.ErrNotFound
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) GetMediaAssetsForUser(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error) {
	if len(ids) == 0 {
		return []domain.MediaAsset{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,kind,storage_key,mime_type,byte_size,created_at FROM media_assets WHERE id=ANY($1::uuid[]) AND user_id=$2 AND deleted_at IS NULL`, ids, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.MediaAsset, len(ids))
	for rows.Next() {
		var asset domain.MediaAsset
		if err := rows.Scan(&asset.ID, &asset.Kind, &asset.StorageKey, &asset.MIMEType, &asset.ByteSize, &asset.CreatedAt); err != nil {
			return nil, err
		}
		byID[asset.ID] = asset
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	assets := make([]domain.MediaAsset, 0, len(ids))
	for _, id := range ids {
		if asset, ok := byID[id]; ok {
			assets = append(assets, asset)
		}
	}
	if len(assets) != len(ids) {
		return nil, repository.ErrNotFound
	}
	return assets, nil
}

// ---- 统一任务队列 ----

const taskSelect = `SELECT id::text,user_id::text,type,payload,status,progress,stage,attempts,last_error,result_ref,created_at,updated_at FROM tasks`

func scanTask(row pgx.Row) (domain.Task, error) {
	var task domain.Task
	err := row.Scan(&task.ID, &task.UserID, &task.Type, &task.Payload, &task.Status, &task.Progress, &task.Stage, &task.Attempts, &task.LastError, &task.ResultRef, &task.CreatedAt, &task.UpdatedAt)
	return task, err
}

func (s *Store) CreateTask(ctx context.Context, userID string, input domain.TaskInput) (domain.Task, error) {
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return domain.Task{}, err
	}
	task, err := scanTask(s.pool.QueryRow(ctx, `
		INSERT INTO tasks(user_id,type,payload,progress,stage)
		VALUES($1,$2,$3,$4,$5) `+taskSelectTail, userID, string(input.Type), payload, input.Progress, input.Stage))
	return task, err
}

// taskSelectTail re-reads the inserted row so CreateTask returns the queued
// task exactly as the polling endpoints will serve it.
const taskSelectTail = `RETURNING id::text,user_id::text,type,payload,status,progress,stage,attempts,last_error,result_ref,created_at,updated_at`

func (s *Store) GetTask(ctx context.Context, userID, taskID string) (domain.Task, error) {
	task, err := scanTask(s.pool.QueryRow(ctx, taskSelect+` WHERE id=$1::uuid AND user_id=$2`, taskID, userID))
	return task, mapNotFound(err)
}

func (s *Store) GetTasksByIDs(ctx context.Context, userID string, ids []string) ([]domain.Task, error) {
	if len(ids) == 0 {
		return []domain.Task{}, nil
	}
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE user_id=$1 AND id=ANY($2::uuid[])`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.Task, len(ids))
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		byID[task.ID] = task
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tasks := make([]domain.Task, 0, len(ids))
	for _, id := range ids {
		if task, ok := byID[id]; ok {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *Store) ActiveTasks(ctx context.Context, userID string, limit int) ([]domain.Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE user_id=$1 AND status IN ('queued','processing') ORDER BY created_at LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) LatestTasksByRef(ctx context.Context, userID string, taskType domain.TaskType, refKey string, refs []string) (map[string]domain.Task, error) {
	result := make(map[string]domain.Task, len(refs))
	if len(refs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (payload->>$3) payload->>$3 AS ref,id::text,user_id::text,type,payload,status,progress,stage,attempts,last_error,result_ref,created_at,updated_at
		FROM tasks
		WHERE user_id=$1 AND type=$2 AND payload->>$3 = ANY($4)
		ORDER BY payload->>$3, created_at DESC`, userID, string(taskType), refKey, refs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var task domain.Task
		if err := rows.Scan(&ref, &task.ID, &task.UserID, &task.Type, &task.Payload, &task.Status, &task.Progress, &task.Stage, &task.Attempts, &task.LastError, &task.ResultRef, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, err
		}
		result[ref] = task
	}
	return result, rows.Err()
}

// taskAttemptsCap mirrors the service retry policy: content jobs (analysis,
// hair preview) get three attempts, image renders two. The SQL CASE keeps the
// zombie reclaimer in sync without a second round trip.
const taskAttemptsCap = `CASE type WHEN 'analysis' THEN 3 WHEN 'hair_preview' THEN 3 ELSE 2 END`

// zombieAnalysisIDs / zombieTaskIDs select the rows a worker died on
// (running with locked_at older than 10 minutes) split by retry budget.
const zombieAnalysisIDs = `
	SELECT (payload->>'analysis_id')::uuid FROM tasks
	WHERE type='analysis' AND status='processing' AND locked_at < now() - interval '10 minutes' AND attempts `

// ClaimTask reclaims zombie rows and locks one queued task of taskType with
// FOR UPDATE SKIP LOCKED. Zombies are running tasks whose locked_at is older
// than 10 minutes: their worker died (release/OOM), so exhausted tasks go to
// the failed terminal state and the rest are requeued. Analysis rows get
// their presentation state reset in the same transaction so client polling
// never gets stuck on "processing".
func (s *Store) ClaimTask(ctx context.Context, taskType domain.TaskType) (domain.Task, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE analyses SET status='failed',error_message='分析暂时没有完成，请稍后重试',stage='分析未完成',updated_at=now()
		WHERE status IN ('queued','processing') AND id IN (`+zombieAnalysisIDs+">= "+taskAttemptsCap+`)`); err != nil {
		return domain.Task{}, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE analyses SET status='queued',updated_at=now()
		WHERE status='processing' AND id IN (`+zombieAnalysisIDs+"< "+taskAttemptsCap+`)`); err != nil {
		return domain.Task{}, false, err
	}
	zombieTasks := `FROM tasks WHERE status='processing' AND locked_at < now() - interval '10 minutes'`
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status='failed',last_error='{"code":"timeout","message":"worker 处理超时，任务已终止"}',updated_at=now()
		WHERE id IN (SELECT id `+zombieTasks+` AND attempts >= `+taskAttemptsCap+`)`); err != nil {
		return domain.Task{}, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status='queued',next_run_at=now(),locked_at=NULL,updated_at=now()
		WHERE id IN (SELECT id `+zombieTasks+` AND attempts < `+taskAttemptsCap+`)`); err != nil {
		return domain.Task{}, false, err
	}
	var task domain.Task
	err = tx.QueryRow(ctx, `
		SELECT id::text,user_id::text,type,payload,status,progress,stage,attempts,last_error,result_ref,created_at,updated_at
		FROM tasks
		WHERE type=$1 AND status='queued' AND next_run_at<=now()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, string(taskType)).
		Scan(&task.ID, &task.UserID, &task.Type, &task.Payload, &task.Status, &task.Progress, &task.Stage, &task.Attempts, &task.LastError, &task.ResultRef, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, false, nil
	}
	if err != nil {
		return domain.Task{}, false, err
	}
	task.Attempts++
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status='processing',attempts=$2,locked_at=now(),updated_at=now() WHERE id=$1`, task.ID, task.Attempts); err != nil {
		return domain.Task{}, false, err
	}
	if task.Type == string(domain.TaskTypeAnalysis) {
		var payload domain.AnalysisTaskPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return domain.Task{}, false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE analyses SET status='processing',progress=GREATEST(progress,15),stage='正在排队等待分析',updated_at=now() WHERE id=$1::uuid`, payload.AnalysisID); err != nil {
			return domain.Task{}, false, err
		}
	}
	return task, true, tx.Commit(ctx)
}

func (s *Store) UpdateTaskProgress(ctx context.Context, taskID string, progress int, stage string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET progress=$2,stage=$3,updated_at=now() WHERE id=$1`, taskID, progress, stage)
	return err
}

func (s *Store) CompleteTask(ctx context.Context, taskID, resultRef string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET status='completed',progress=100,result_ref=$2,updated_at=now() WHERE id=$1`, taskID, resultRef)
	return err
}

func (s *Store) FailTask(ctx context.Context, taskID, code, message string, photoReasons []string, retryAt time.Time) error {
	encoded := encodeTaskError(code, message, photoReasons)
	if retryAt.IsZero() {
		_, err := s.pool.Exec(ctx, `UPDATE tasks SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, taskID, encoded)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET status='queued',last_error=$2,next_run_at=$3,locked_at=NULL,updated_at=now() WHERE id=$1`, taskID, encoded, retryAt)
	return err
}

// encodeTaskError packs the failure code and photo rejection reasons into
// last_error without extra columns: structured failures are stored as a JSON
// envelope; transient requeues keep the plain provider error for operators.
func encodeTaskError(code, message string, photoReasons []string) string {
	if code == "" && len(photoReasons) == 0 {
		return message
	}
	data, err := json.Marshal(struct {
		Code         string   `json:"code,omitempty"`
		Message      string   `json:"message"`
		PhotoReasons []string `json:"photo_reasons,omitempty"`
	}{Code: code, Message: message, PhotoReasons: photoReasons})
	if err != nil {
		return message
	}
	return string(data)
}

func (s *Store) HealthJobs(ctx context.Context) (domain.JobsHealth, error) {
	var health domain.JobsHealth
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT coalesce(extract(epoch FROM (now()-min(created_at)))::bigint, 0) FROM tasks WHERE status='queued'),
			(SELECT count(*)::bigint FROM tasks WHERE status='failed' AND updated_at>now() - interval '1 hour')`).
		Scan(&health.OldestQueuedSeconds, &health.FailedLastHour)
	return health, err
}

// ---- 分析与报告 ----

func (s *Store) CreateAnalysis(ctx context.Context, userID string, input domain.CreateAnalysisInput) (domain.Analysis, *domain.Task, error) {
	if len(input.MediaIDs) != 3 {
		return domain.Analysis{}, nil, fmt.Errorf("exactly three media assets are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	defer tx.Rollback(ctx)
	// Only one analysis may be active for a user at a time. The advisory lock
	// closes the small race between two create requests, while returning the
	// existing job makes retries and returning from a backgrounded client safe.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "analysis:"+userID); err != nil {
		return domain.Analysis{}, nil, err
	}
	var active domain.Analysis
	err = tx.QueryRow(ctx, `
		SELECT id::text,status,progress,stage,scene,media_ids::text[],coalesce((SELECT t.id::text FROM tasks t WHERE t.type='analysis' AND t.payload->>'analysis_id'=a.id::text AND t.status IN ('queued','processing') ORDER BY t.created_at DESC LIMIT 1),''),created_at,updated_at
		FROM analyses a
		WHERE user_id=$1 AND status IN ('queued','processing')
		ORDER BY created_at DESC LIMIT 1`, userID).
		Scan(&active.ID, &active.Status, &active.Progress, &active.Stage, &active.Scene, &active.MediaIDs, &active.TaskID, &active.CreatedAt, &active.UpdatedAt)
	if err == nil {
		var task *domain.Task
		if active.TaskID != "" {
			claimed, taskErr := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE id=$1::uuid`, active.TaskID))
			if taskErr == nil {
				task = &claimed
			}
		}
		return active, task, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Analysis{}, nil, err
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM media_assets WHERE user_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL`, userID, input.MediaIDs).Scan(&count)
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	if count != 3 {
		return domain.Analysis{}, nil, fmt.Errorf("one or more media assets are unavailable")
	}
	profile, err := json.Marshal(input.Profile)
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	var analysis domain.Analysis
	err = tx.QueryRow(ctx, `
		INSERT INTO analyses(user_id,scene,media_ids,profile,status,progress,stage)
		VALUES($1,$2,$3,$4,'queued',5,'正在安全上传照片')
		RETURNING id::text,status,progress,stage,scene,created_at,updated_at`, userID, input.Scene, input.MediaIDs, profile).
		Scan(&analysis.ID, &analysis.Status, &analysis.Progress, &analysis.Stage, &analysis.Scene, &analysis.CreatedAt, &analysis.UpdatedAt)
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	task, err := scanTask(tx.QueryRow(ctx, `
		INSERT INTO tasks(user_id,type,payload,progress,stage)
		VALUES($1,'analysis',$2,5,'正在安全上传照片') `+taskSelectTail,
		userID, domain.AnalysisTaskPayload{AnalysisID: analysis.ID}))
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	analysis.TaskID = task.ID
	return analysis, &task, tx.Commit(ctx)
}

func (s *Store) GetAnalysis(ctx context.Context, userID, analysisID string) (domain.Analysis, error) {
	var item domain.Analysis
	err := s.pool.QueryRow(ctx, `
		SELECT a.id::text,a.status,a.progress,a.stage,a.scene,a.media_ids::text[],a.error_message,coalesce(r.id::text,''),coalesce((SELECT t.id::text FROM tasks t WHERE t.type='analysis' AND t.payload->>'analysis_id'=a.id::text ORDER BY t.created_at DESC LIMIT 1),''),a.created_at,a.updated_at
		FROM analyses a LEFT JOIN reports r ON r.analysis_id=a.id
		WHERE a.id=$1::uuid AND a.user_id=$2`, analysisID, userID).
		Scan(&item.ID, &item.Status, &item.Progress, &item.Stage, &item.Scene, &item.MediaIDs, &item.ErrorMessage, &item.ReportID, &item.TaskID, &item.CreatedAt, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) GetAnalysisInput(ctx context.Context, userID, analysisID string) (domain.CreateAnalysisInput, error) {
	var input domain.CreateAnalysisInput
	var profileData []byte
	err := s.pool.QueryRow(ctx, `
		SELECT media_ids::text[],scene,profile FROM analyses
		WHERE id=$1::uuid AND user_id=$2`, analysisID, userID).
		Scan(&input.MediaIDs, &input.Scene, &profileData)
	if err != nil {
		return domain.CreateAnalysisInput{}, mapNotFound(err)
	}
	if len(profileData) > 0 {
		_ = json.Unmarshal(profileData, &input.Profile)
	}
	return input, nil
}

func (s *Store) UpdateAnalysisProgress(ctx context.Context, analysisID string, progress int, stage string) error {
	_, err := s.pool.Exec(ctx, `UPDATE analyses SET progress=$2,stage=$3,updated_at=now() WHERE id=$1::uuid`, analysisID, progress, stage)
	return err
}

func (s *Store) CompleteAnalysis(ctx context.Context, userID, analysisID string, output domain.AnalysisOutput) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var reportID string
	err = tx.QueryRow(ctx, `
		INSERT INTO reports(analysis_id,user_id,current_image_url,impression_tags,priority_title,priority_copy,provider_version)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, analysisID, userID, output.CurrentImageURL, output.ImpressionTags, output.PriorityTitle, output.PriorityCopy, output.ProviderVersion).Scan(&reportID)
	if err != nil {
		if isForeignKeyViolation(err, "reports_analysis_id_fkey") {
			return "", repository.ErrTaskRemoved
		}
		return "", err
	}
	for index, finding := range output.Findings {
		_, err = tx.Exec(ctx, `INSERT INTO report_findings(report_id,label,category,severity,detail,photo,anchor_x,anchor_y,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, reportID, finding.Label, finding.Category, finding.Severity, finding.Detail, finding.Photo, finding.AnchorX, finding.AnchorY, index+1)
		if err != nil {
			return "", err
		}
	}
	// 报告与方案解耦：分析只落报告（含 findings），general 方案组由
	// plan_group 任务从报告内容生成（见 service.processPlanGroup）。
	_, err = tx.Exec(ctx, `UPDATE analyses SET status='completed',progress=100,stage='形象报告已经准备好',updated_at=now() WHERE id=$1::uuid`, analysisID)
	if err != nil {
		return "", err
	}
	return reportID, tx.Commit(ctx)
}

func (s *Store) FailAnalysisPresentation(ctx context.Context, analysisID, stage, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE analyses SET status='failed',stage=$2,error_message=$3,updated_at=now() WHERE id=$1::uuid`, analysisID, stage, message)
	return err
}

func (s *Store) GetReport(ctx context.Context, userID, reportID string) (domain.Report, error) {
	var report domain.Report
	err := s.pool.QueryRow(ctx, `SELECT id::text,analysis_id::text,current_image_url,impression_tags,priority_title,priority_copy,provider_version,generated_at FROM reports WHERE id=$1::uuid AND user_id=$2`, reportID, userID).
		Scan(&report.ID, &report.AnalysisID, &report.CurrentImageURL, &report.ImpressionTags, &report.PriorityTitle, &report.PriorityCopy, &report.ProviderVersion, &report.GeneratedAt)
	if err != nil {
		return report, mapNotFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,label,category,severity,detail,photo,anchor_x,anchor_y FROM report_findings WHERE report_id=$1::uuid ORDER BY sort_order`, reportID)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var finding domain.Finding
		if err := rows.Scan(&finding.ID, &finding.Label, &finding.Category, &finding.Severity, &finding.Detail, &finding.Photo, &finding.AnchorX, &finding.AnchorY); err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, finding)
	}
	return report, rows.Err()
}

func (s *Store) LatestReport(ctx context.Context, userID string) (domain.Report, error) {
	var reportID string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM reports WHERE user_id=$1 ORDER BY generated_at DESC LIMIT 1`, userID).Scan(&reportID)
	if err != nil {
		return domain.Report{}, mapNotFound(err)
	}
	return s.GetReport(ctx, userID, reportID)
}

// ---- 方案 ----

const planSelect = `SELECT p.id::text,p.report_id::text,p.scene,p.name,p.slug,p.image_url,p.recommended,p.descriptor,p.why,p.outcome_tags,p.difference_tags,p.sort_order,(p.selected_at IS NOT NULL),r.current_image_url,p.generated_image_url,p.look_provider FROM plans p JOIN reports r ON r.id=p.report_id`

func scanPlan(row pgx.Row) (domain.Plan, error) {
	var item domain.Plan
	err := row.Scan(&item.ID, &item.ReportID, &item.Scene, &item.Name, &item.Slug, &item.ImageURL, &item.Recommended, &item.Descriptor, &item.Why, &item.OutcomeTags, &item.DifferenceTags, &item.Sort, &item.Selected, &item.CurrentImageURL, &item.GeneratedImageURL, &item.LookProvider)
	return item, err
}

func (s *Store) ListPlans(ctx context.Context, userID, reportID, scene string) ([]domain.Plan, error) {
	// An empty scene returns every group of the report (contract: 缺省返回全部分组).
	query, args := planSelect+` WHERE p.report_id=$1::uuid AND p.user_id=$2 ORDER BY p.scene,p.sort_order`, []any{reportID, userID}
	if scene != "" {
		query, args = planSelect+` WHERE p.report_id=$1::uuid AND p.user_id=$2 AND p.scene=$3 ORDER BY p.sort_order`, append(args, scene)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Plan, 0, 3)
	for rows.Next() {
		item, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpsertScenePlans is the idempotent engine of PUT /v1/reports/{id}/plans.
// The reports row lock serializes concurrent double-submits; an unchanged
// brief keeps the existing rows untouched (so selection and checklists
// survive), while a changed brief rewrites copy, steps and clears the stale
// generated image so a fresh look task is enqueued by the caller.
func (s *Store) UpsertScenePlans(ctx context.Context, userID, reportID string, input domain.ScenePlanInput, plans []domain.Plan) ([]domain.Plan, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var lockedReport string
	err = tx.QueryRow(ctx, `SELECT id::text FROM reports WHERE id=$1::uuid AND user_id=$2 FOR UPDATE`, reportID, userID).Scan(&lockedReport)
	if err != nil {
		return nil, mapNotFound(err)
	}
	briefData, err := json.Marshal(map[string]any{"scene": input.Scene, "answers": input.Answers})
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		var planID, storedBrief string
		err := tx.QueryRow(ctx, `SELECT id::text,scene_brief::text FROM plans WHERE report_id=$1::uuid AND user_id=$2 AND scene=$3 AND slug=$4`, reportID, userID, input.Scene, plan.Slug).Scan(&planID, &storedBrief)
		switch {
		case err == nil:
			if storedBrief == string(briefData) {
				continue
			}
			if _, err = tx.Exec(ctx, `
				UPDATE plans SET scene_brief=$3,name=$4,image_url=$5,recommended=$6,descriptor=$7,why=$8,outcome_tags=$9,difference_tags=$10,sort_order=$11,
					generated_image_url='',generated_storage_key='',look_provider=''
				WHERE id=$1::uuid AND user_id=$2`, planID, userID, briefData, plan.Name, plan.ImageURL, plan.Recommended, plan.Descriptor, plan.Why, plan.OutcomeTags, plan.DifferenceTags, plan.Sort); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM plan_steps WHERE plan_id=$1::uuid`, planID); err != nil {
				return nil, err
			}
		case errors.Is(err, pgx.ErrNoRows):
			err = tx.QueryRow(ctx, `
				INSERT INTO plans(report_id,user_id,scene,scene_brief,name,slug,image_url,recommended,descriptor,why,outcome_tags,difference_tags,sort_order)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id::text`,
				reportID, userID, input.Scene, briefData, plan.Name, plan.Slug, plan.ImageURL, plan.Recommended, plan.Descriptor, plan.Why, plan.OutcomeTags, plan.DifferenceTags, plan.Sort).Scan(&planID)
			if err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
		for _, step := range plan.Steps {
			if _, err = tx.Exec(ctx, `INSERT INTO plan_steps(plan_id,category,title,summary,details,sort_order) VALUES($1,$2,$3,$4,$5,$6)`, planID, step.Category, step.Title, step.Summary, step.Details, step.Sort); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListPlans(ctx, userID, reportID, input.Scene)
}

func (s *Store) GetPlan(ctx context.Context, userID, planID string) (domain.Plan, error) {
	item, err := scanPlan(s.pool.QueryRow(ctx, planSelect+` WHERE p.id=$1::uuid AND p.user_id=$2`, planID, userID))
	if err != nil {
		return item, mapNotFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,category,title,summary,details,sort_order FROM plan_steps WHERE plan_id=$1::uuid ORDER BY sort_order`, planID)
	if err != nil {
		return item, err
	}
	defer rows.Close()
	for rows.Next() {
		var step domain.PlanStep
		if err := rows.Scan(&step.ID, &step.Category, &step.Title, &step.Summary, &step.Details, &step.Sort); err != nil {
			return item, err
		}
		item.Steps = append(item.Steps, step)
	}
	return item, rows.Err()
}

func (s *Store) ApplyPlanLookResult(ctx context.Context, planID, resultURL, storageKey, providerVersion string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE plans SET generated_image_url=$2,generated_storage_key=$3,look_provider=$4 WHERE id=$1::uuid`, planID, resultURL, storageKey, providerVersion)
	if err == nil && tag.RowsAffected() == 0 {
		return repository.ErrTaskRemoved
	}
	return err
}

func (s *Store) GetPlanLookJob(ctx context.Context, userID, planID string) (domain.PlanLookJob, error) {
	var job domain.PlanLookJob
	err := s.pool.QueryRow(ctx, `
		SELECT p.id::text,p.report_id::text,p.user_id::text,p.name,p.slug,p.why,a.media_ids::text[]
		FROM plans p
		JOIN reports r ON r.id=p.report_id
		JOIN analyses a ON a.id=r.analysis_id
		WHERE p.id=$1::uuid AND p.user_id=$2`, planID, userID).
		Scan(&job.PlanID, &job.ReportID, &job.UserID, &job.Name, &job.Slug, &job.Why, &job.MediaIDs)
	if err != nil {
		return job, mapNotFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT category,title,summary FROM plan_steps WHERE plan_id=$1::uuid ORDER BY sort_order`, job.PlanID)
	if err != nil {
		return job, err
	}
	defer rows.Close()
	for rows.Next() {
		var step domain.PlanStep
		if err := rows.Scan(&step.Category, &step.Title, &step.Summary); err != nil {
			return job, err
		}
		job.Steps = append(job.Steps, step)
	}
	return job, rows.Err()
}

func (s *Store) SelectPlan(ctx context.Context, userID, planID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reportID, scene string
	if err = tx.QueryRow(ctx, `SELECT report_id::text,scene FROM plans WHERE id=$1::uuid AND user_id=$2 FOR UPDATE`, planID, userID).Scan(&reportID, &scene); err != nil {
		return mapNotFound(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE plans SET selected_at=NULL WHERE report_id=$1::uuid AND scene=$2`, reportID, scene); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE plans SET selected_at=now() WHERE id=$1::uuid`, planID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO checklist_items(plan_id,user_id,category,title,description,meta,sort_order)
		SELECT ps.plan_id,$2,ps.category,ps.title,ps.summary,
		CASE ps.category WHEN 'hair' THEN '给发型师看参考卡' WHEN 'makeup' THEN '预计 8 分钟' ELSE '优先使用现有衣橱' END,
		ps.sort_order FROM plan_steps ps WHERE ps.plan_id=$1::uuid
		ON CONFLICT(plan_id,category) DO NOTHING`, planID, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetChecklist(ctx context.Context, userID, planID string) ([]domain.ChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,plan_id::text,category,title,description,meta,completed,sort_order FROM checklist_items WHERE plan_id=$1::uuid AND user_id=$2 ORDER BY sort_order`, planID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ChecklistItem{}
	for rows.Next() {
		var item domain.ChecklistItem
		if err := rows.Scan(&item.ID, &item.PlanID, &item.Category, &item.Title, &item.Description, &item.Meta, &item.Completed, &item.Sort); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SetChecklistItem(ctx context.Context, userID, planID, itemID string, completed bool) (domain.ChecklistItem, error) {
	var item domain.ChecklistItem
	err := s.pool.QueryRow(ctx, `
		UPDATE checklist_items SET completed=$4,updated_at=now()
		WHERE id=$1::uuid AND user_id=$2 AND plan_id=$3::uuid
		RETURNING id::text,plan_id::text,category,title,description,meta,completed,sort_order`, itemID, userID, planID, completed).
		Scan(&item.ID, &item.PlanID, &item.Category, &item.Title, &item.Description, &item.Meta, &item.Completed, &item.Sort)
	return item, mapNotFound(err)
}

func (s *Store) AddFeedback(ctx context.Context, userID, planID string, input domain.FeedbackInput) error {
	if _, err := uuid.Parse(planID); err != nil {
		return repository.ErrNotFound
	}
	var mediaID any
	if input.MediaID != "" {
		if _, err := uuid.Parse(input.MediaID); err != nil {
			return repository.ErrNotFound
		}
		mediaID = input.MediaID
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO feedback(user_id,plan_id,tags,comment,media_id) SELECT $1,p.id,$3,$4,$5 FROM plans p WHERE p.id=$2::uuid AND p.user_id=$1`, userID, planID, input.Tags, input.Comment, mediaID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ---- 诊断 ----

func (s *Store) CreateDiagnostic(ctx context.Context, userID string, input domain.DiagnosticInput, result domain.ToolResult) (domain.ToolResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ToolResult{}, err
	}
	defer tx.Rollback(ctx)
	var reportID any
	if input.ReportID != "" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=$1::uuid AND user_id=$2)`, input.ReportID, userID).Scan(&exists); err != nil {
			return domain.ToolResult{}, err
		}
		if !exists {
			return domain.ToolResult{}, repository.ErrNotFound
		}
		reportID = input.ReportID
	}
	var mediaID any
	if input.MediaID != "" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE id=$1::uuid AND user_id=$2 AND deleted_at IS NULL)`, input.MediaID, userID).Scan(&exists); err != nil {
			return domain.ToolResult{}, err
		}
		if !exists {
			return domain.ToolResult{}, repository.ErrNotFound
		}
		mediaID = input.MediaID
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return domain.ToolResult{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO tool_results(user_id,report_id,media_id,kind,scene,payload)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id::text,created_at`, userID, reportID, mediaID, input.Kind, input.Scene, payload).
		Scan(&result.ID, &result.CreatedAt)
	if err != nil {
		return domain.ToolResult{}, err
	}
	result.MediaID = input.MediaID
	return result, tx.Commit(ctx)
}

const diagnosticSelect = `SELECT id::text,kind,scene,payload,saved,created_at,coalesce(media_id::text,'') FROM tool_results`

func scanDiagnostic(row pgx.Row) (domain.ToolResult, error) {
	var result domain.ToolResult
	var payload []byte
	var id, kind, scene, mediaID string
	var saved bool
	var createdAt time.Time
	if err := row.Scan(&id, &kind, &scene, &payload, &saved, &createdAt, &mediaID); err != nil {
		return domain.ToolResult{}, mapNotFound(err)
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return domain.ToolResult{}, err
	}
	result.ID = id
	result.Kind = kind
	result.Scene = scene
	result.Saved = saved
	result.CreatedAt = createdAt
	result.MediaID = mediaID
	return result, nil
}

func (s *Store) GetDiagnostic(ctx context.Context, userID, diagnosticID string) (domain.ToolResult, error) {
	return scanDiagnostic(s.pool.QueryRow(ctx, diagnosticSelect+` WHERE id=$1::uuid AND user_id=$2`, diagnosticID, userID))
}

func (s *Store) LatestDiagnostic(ctx context.Context, userID, kind string) (domain.ToolResult, error) {
	return scanDiagnostic(s.pool.QueryRow(ctx, diagnosticSelect+` WHERE user_id=$1 AND kind=$2 ORDER BY created_at DESC LIMIT 1`, userID, kind))
}

func (s *Store) SetDiagnosticSaved(ctx context.Context, userID, diagnosticID string, saved bool) (domain.ToolResult, error) {
	result, err := scanDiagnostic(s.pool.QueryRow(ctx, `
		UPDATE tool_results SET saved=$3,updated_at=now()
		WHERE id=$1::uuid AND user_id=$2
		RETURNING id::text,kind,scene,payload,saved,created_at,coalesce(media_id::text,'')`, diagnosticID, userID, saved))
	if err != nil {
		return domain.ToolResult{}, err
	}
	result.Saved = saved
	return result, nil
}

// ---- 发型预览 ----

const hairPreviewSelect = `SELECT id::text,style_id,style_name,scene,source_image_url,result_image_url,provider_version,saved,created_at,updated_at FROM hair_previews`

func scanHairPreview(row pgx.Row) (domain.HairPreview, error) {
	var item domain.HairPreview
	err := row.Scan(&item.ID, &item.StyleID, &item.StyleName, &item.Scene, &item.SourceImageURL, &item.ResultImageURL, &item.ProviderVersion, &item.Saved, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) CreateHairPreview(ctx context.Context, userID string, input domain.HairPreviewInput, styleName string) (domain.HairPreview, *domain.Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.HairPreview{}, nil, err
	}
	defer tx.Rollback(ctx)
	// A hair preview is a user-level asynchronous capability. Serialize its
	// creation and return the current active job so repeated taps, page reloads,
	// or a second device cannot create concurrent previews of the same type.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "hair-preview:"+userID); err != nil {
		return domain.HairPreview{}, nil, err
	}
	existing, existingErr := scanHairPreview(tx.QueryRow(ctx, hairPreviewSelect+`
		WHERE user_id=$1 AND id IN (SELECT (payload->>'preview_id')::uuid FROM tasks WHERE type='hair_preview' AND status IN ('queued','processing'))
		ORDER BY created_at DESC LIMIT 1`, userID))
	if existingErr == nil {
		task, taskErr := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE type='hair_preview' AND payload->>'preview_id'=$1 AND status IN ('queued','processing') ORDER BY created_at DESC LIMIT 1`, existing.ID))
		if taskErr != nil {
			return domain.HairPreview{}, nil, taskErr
		}
		return existing, &task, tx.Commit(ctx)
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.HairPreview{}, nil, existingErr
	}
	var storageKey string
	if err = tx.QueryRow(ctx, `SELECT storage_key FROM media_assets WHERE id=$1::uuid AND user_id=$2 AND kind='face' AND deleted_at IS NULL`, input.MediaID, userID).Scan(&storageKey); err != nil {
		return domain.HairPreview{}, nil, mapNotFound(err)
	}
	var reportID any
	if input.ReportID != "" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=$1::uuid AND user_id=$2)`, input.ReportID, userID).Scan(&exists); err != nil {
			return domain.HairPreview{}, nil, err
		}
		if !exists {
			return domain.HairPreview{}, nil, repository.ErrNotFound
		}
		reportID = input.ReportID
	}
	sourceURL := "/uploads/" + storageKey
	if len(storageKey) >= 5 && storageKey[:5] == "demo/" {
		sourceURL = "/assets/looks/natural.png"
	}
	var preview domain.HairPreview
	err = tx.QueryRow(ctx, `
		INSERT INTO hair_previews(user_id,report_id,media_id,scene,style_id,style_name,source_image_url)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id::text,style_id,style_name,scene,source_image_url,result_image_url,provider_version,saved,created_at,updated_at`,
		userID, reportID, input.MediaID, input.Scene, input.StyleID, styleName, sourceURL).
		Scan(&preview.ID, &preview.StyleID, &preview.StyleName, &preview.Scene, &preview.SourceImageURL, &preview.ResultImageURL, &preview.ProviderVersion, &preview.Saved, &preview.CreatedAt, &preview.UpdatedAt)
	if err != nil {
		return domain.HairPreview{}, nil, err
	}
	task, err := scanTask(tx.QueryRow(ctx, `
		INSERT INTO tasks(user_id,type,payload,stage)
		VALUES($1,'hair_preview',$2,'正在排队') `+taskSelectTail,
		userID, domain.HairPreviewTaskPayload{PreviewID: preview.ID}))
	if err != nil {
		return domain.HairPreview{}, nil, err
	}
	return preview, &task, tx.Commit(ctx)
}

func (s *Store) GetHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	item, err := scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelect+` WHERE id=$1::uuid AND user_id=$2`, previewID, userID))
	return item, mapNotFound(err)
}

// GetActiveHairPreview mirrors the active-preview lookup inside
// CreateHairPreview: the preview whose hair_preview task is still queued or
// processing. No active job maps to repository.ErrNotFound.
func (s *Store) GetActiveHairPreview(ctx context.Context, userID string) (domain.HairPreview, error) {
	item, err := scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelect+`
		WHERE user_id=$1 AND id IN (SELECT (payload->>'preview_id')::uuid FROM tasks WHERE type='hair_preview' AND status IN ('queued','processing'))
		ORDER BY created_at DESC LIMIT 1`, userID))
	return item, mapNotFound(err)
}

func (s *Store) GetHairPreviewInput(ctx context.Context, userID, previewID string) (domain.HairPreviewInput, error) {
	var input domain.HairPreviewInput
	err := s.pool.QueryRow(ctx, `SELECT media_id::text,coalesce(report_id::text,''),style_id,scene FROM hair_previews WHERE id=$1::uuid AND user_id=$2`, previewID, userID).
		Scan(&input.MediaID, &input.ReportID, &input.StyleID, &input.Scene)
	return input, mapNotFound(err)
}

func (s *Store) ListSavedHairPreviews(ctx context.Context, userID string) ([]domain.HairPreview, error) {
	rows, err := s.pool.Query(ctx, hairPreviewSelect+` WHERE user_id=$1 AND saved=true AND result_image_url<>'' ORDER BY updated_at DESC LIMIT 20`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.HairPreview, 0)
	for rows.Next() {
		item, err := scanHairPreview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ApplyHairPreviewResult(ctx context.Context, previewID, resultURL, storageKey, providerVersion string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE hair_previews SET result_image_url=$2,result_storage_key=$3,provider_version=$4,updated_at=now() WHERE id=$1::uuid`, previewID, resultURL, storageKey, providerVersion)
	if err == nil && tag.RowsAffected() == 0 {
		return repository.ErrTaskRemoved
	}
	return err
}

func (s *Store) SaveHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	item, err := scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelect+` WHERE id=$1::uuid AND user_id=$2 AND result_image_url<>''`, previewID, userID))
	if err != nil {
		return domain.HairPreview{}, mapNotFound(err)
	}
	item.Saved = true
	_, err = s.pool.Exec(ctx, `UPDATE hair_previews SET saved=true,updated_at=now() WHERE id=$1::uuid AND user_id=$2`, previewID, userID)
	return item, err
}

// deleteUserDataQueries 按依赖顺序列出注销账号时需要清理的用户数据。
// users 必须放在最后：user_sessions 删除后旧 token 立即失效，而迁移里的
// ON DELETE CASCADE 只在 users 行被删除时才会触发。
var deleteUserDataQueries = []string{
	`DELETE FROM billing_ledger WHERE user_id=$1`,
	`DELETE FROM billing_usage WHERE user_id=$1`,
	`DELETE FROM billing_orders WHERE user_id=$1`,
	`DELETE FROM billing_wallets WHERE user_id=$1`,
	`DELETE FROM share_cards WHERE user_id=$1`,
	`DELETE FROM today_plans WHERE user_id=$1`,
	`DELETE FROM tasks WHERE user_id=$1`,
	`DELETE FROM wardrobe_outfits WHERE user_id=$1`,
	`DELETE FROM wardrobe_items WHERE user_id=$1`,
	`DELETE FROM advisor_conversations WHERE user_id=$1`,
	`DELETE FROM product_events WHERE user_id=$1`,
	`DELETE FROM tool_results WHERE user_id=$1`,
	`DELETE FROM analyses WHERE user_id=$1`,
	`DELETE FROM hair_previews WHERE user_id=$1`,
	`DELETE FROM media_assets WHERE user_id=$1`,
	`DELETE FROM feedback WHERE user_id=$1`,
	`DELETE FROM user_identities WHERE user_id=$1`,
	`DELETE FROM user_profiles WHERE user_id=$1`,
	`DELETE FROM user_sessions WHERE user_id=$1`,
	`DELETE FROM users WHERE id=$1`,
}

func (s *Store) DeleteUserData(ctx context.Context, userID string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT storage_key FROM media_assets WHERE user_id=$1 AND deleted_at IS NULL
		UNION ALL SELECT result_storage_key FROM hair_previews WHERE user_id=$1 AND result_storage_key<>''
		UNION ALL SELECT generated_storage_key FROM plans WHERE user_id=$1 AND generated_storage_key<>''
		UNION ALL SELECT generated_storage_key FROM today_plans WHERE user_id=$1 AND generated_storage_key<>''`, userID)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	rows.Close()
	for _, query := range deleteUserDataQueries {
		if _, err = tx.Exec(ctx, query, userID); err != nil {
			return nil, err
		}
	}
	return keys, tx.Commit(ctx)
}

func newUUID() string { return uuid.NewString() }
