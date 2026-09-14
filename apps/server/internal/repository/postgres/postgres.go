package postgres

import (
	"github.com/google/uuid"

	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

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
	// baseline 的 users 没有 open_id：身份唯一性由
	// user_identities(provider,identifier) UNIQUE 承担，上面的按身份查询
	// 未命中即首次登录，直接建用户再绑身份。
	var userID string
	err = tx.QueryRow(ctx, `INSERT INTO users(nickname) VALUES($1) RETURNING id::text`, nickname).Scan(&userID)
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
	err := s.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES($1) RETURNING id::text,nickname`,
		nickname).Scan(&user.ID, &user.Nickname)
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
		SELECT id::text,user_id::text,role,COALESCE(height_cm,0),budget,updated_at
		FROM user_profiles WHERE user_id=$1`, userID).
		Scan(&profile.ID, &profile.UserID, &profile.Role, &profile.HeightCM, &profile.Budget, &profile.UpdatedAt)
	return profile, mapNotFound(err)
}

func (s *Store) UpdateUserAvatar(ctx context.Context, userID, mediaID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET avatar_media_id=$2::uuid,updated_at=now()
		WHERE id=$1::uuid AND EXISTS (
			SELECT 1 FROM media_assets WHERE id=$2::uuid AND user_id=$1::uuid AND deleted_at IS NULL
		)`, userID, mediaID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (s *Store) GetUserAvatar(ctx context.Context, userID string) (domain.MediaAsset, error) {
	var item domain.MediaAsset
	err := s.pool.QueryRow(ctx, `
		SELECT m.id::text,m.purpose,m.object_key,m.mime_type,m.byte_size,m.created_at
		FROM users u
		JOIN media_assets m ON m.id=u.avatar_media_id
		WHERE u.id=$1::uuid AND m.deleted_at IS NULL`, userID).
		Scan(&item.ID, &item.Purpose, &item.ObjectKey, &item.MIMEType, &item.ByteSize, &item.CreatedAt)
	return item, mapNotFound(err)
}

func (s *Store) UpdateUserNickname(ctx context.Context, userID, nickname string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET nickname=$2 WHERE id=$1::uuid`, userID, nickname)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (s *Store) SaveUserProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error) {
	var height any
	if profile.HeightCM >= 100 {
		height = profile.HeightCM
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_profiles(user_id,height_cm,role,budget)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(user_id) DO UPDATE SET
			height_cm=EXCLUDED.height_cm,role=EXCLUDED.role,budget=EXCLUDED.budget,updated_at=now()
		RETURNING id::text,user_id::text,updated_at`, userID, height, profile.Role, profile.Budget).
		Scan(&profile.ID, &profile.UserID, &profile.UpdatedAt)
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

func (s *Store) GetMediaAssets(ctx context.Context, ids []string) ([]domain.MediaAsset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text,purpose,object_key,mime_type,byte_size,created_at
		FROM media_assets WHERE id=ANY($1::uuid[]) AND deleted_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.MediaAsset, len(ids))
	for rows.Next() {
		var item domain.MediaAsset
		if err := rows.Scan(&item.ID, &item.Purpose, &item.ObjectKey, &item.MIMEType, &item.ByteSize, &item.CreatedAt); err != nil {
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
	rows, err := s.pool.Query(ctx, `SELECT id::text,purpose,object_key,mime_type,byte_size,created_at FROM media_assets WHERE id=ANY($1::uuid[]) AND user_id=$2 AND deleted_at IS NULL`, ids, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]domain.MediaAsset, len(ids))
	for rows.Next() {
		var asset domain.MediaAsset
		if err := rows.Scan(&asset.ID, &asset.Purpose, &asset.ObjectKey, &asset.MIMEType, &asset.ByteSize, &asset.CreatedAt); err != nil {
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

type prefixedRow struct {
	prefix []any
	row    interface{ Scan(dest ...any) error }
}

func (r prefixedRow) Scan(dest ...any) error {
	return r.row.Scan(append(append([]any{}, r.prefix...), dest...)...)
}

func scanTask(row interface{ Scan(dest ...any) error }) (domain.Task, error) {
	var task domain.Task
	var progress, attempts int
	var status, stage, resultRef string
	var lastError *string
	err := row.Scan(&task.ID, &task.UserID, &task.Type, &task.Payload, &status, &progress, &stage, &attempts, &lastError, &resultRef, &task.CreatedAt, &task.UpdatedAt)
	task.Status = domain.TaskStatus(status)
	task.ProgressBPS = progress
	task.StageCode = stage
	task.Attempt = attempts
	if lastError != nil {
		task.ErrorCode = *lastError
	}
	return task, err
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

// newUUID 生成带连字符的 v4 UUID（account/me 写路径用）。
func newUUID() string { return uuid.NewString() }
