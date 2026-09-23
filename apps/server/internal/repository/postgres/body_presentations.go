package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
)

// storedOrbitFrame 是 body_presentations.frames 的 jsonb 形状：只持久化
// yaw 与 COS object key，URL 由服务层在读取时签名投影（库不存签名 URL）。
type storedOrbitFrame struct {
	Yaw        float64 `json:"yaw"`
	StorageKey string  `json:"storage_key"`
}

const bodyPresentationSelect = `SELECT id::text,body_media_id::text,face_media_id::text,representation,video_storage_key,duration_ms,frames,mesh,provider_version,created_at,updated_at FROM body_presentations`

func scanStoredBodyPresentation(row pgx.Row) (domain.StoredBodyPresentation, error) {
	var out domain.StoredBodyPresentation
	item := &out.Presentation
	var framesRaw, meshRaw []byte
	err := row.Scan(
		&item.ID, &item.BodyMediaID, &item.FaceMediaID, &item.Representation,
		&out.VideoStorageKey, &item.Orbit.DurationMS, &framesRaw, &meshRaw,
		&item.ProviderVersion, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return out, err
	}
	if len(framesRaw) > 0 && string(framesRaw) != "null" {
		var stored []storedOrbitFrame
		if err := json.Unmarshal(framesRaw, &stored); err != nil {
			return out, err
		}
		frames := make([]domain.OrbitFrame, 0, len(stored))
		out.FrameKeys = make([]string, 0, len(stored))
		for _, frame := range stored {
			frames = append(frames, domain.OrbitFrame{Yaw: frame.Yaw})
			out.FrameKeys = append(out.FrameKeys, frame.StorageKey)
		}
		item.Orbit.Frames = frames
	}
	if item.Orbit.Frames == nil {
		item.Orbit.Frames = []domain.OrbitFrame{}
	}
	if len(meshRaw) > 0 && string(meshRaw) != "null" {
		var mesh domain.BodyMesh
		if err := json.Unmarshal(meshRaw, &mesh); err != nil {
			return out, err
		}
		item.Mesh = &mesh
	}
	return out, nil
}

// CreateBodyPresentation 在同一事务里建展示资源、公开操作与 body_orbit 任务；
// 同一用户已有进行中任务时返回既有资源（咨询锁防并发双建）。
func (s *Store) CreateBodyPresentation(ctx context.Context, userID string, input domain.BodyPresentationInput, maxAttempts int) (domain.CreatedBodyPresentation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "body-orbit:"+userID); err != nil {
		return domain.CreatedBodyPresentation{}, err
	}

	var existingID string
	existingErr := tx.QueryRow(ctx, `
		SELECT p.id FROM body_presentations p
		JOIN tasks t ON t.type='body_orbit' AND t.subject_id=p.id AND t.user_id=p.user_id
		WHERE p.user_id=$1::uuid AND t.status IN ('queued','leased','retry_wait')
		ORDER BY t.created_at DESC LIMIT 1`, userID).Scan(&existingID)
	if existingErr == nil {
		item, err := scanStoredBodyPresentation(tx.QueryRow(ctx, bodyPresentationSelect+` WHERE id=$1::uuid AND user_id=$2::uuid`, existingID, userID))
		if err != nil {
			return domain.CreatedBodyPresentation{}, err
		}
		task, err := scanBodyOrbitTask(tx.QueryRow(ctx, bodyOrbitTaskSelect+` WHERE user_id=$1::uuid AND subject_id=$2::uuid AND status IN ('queued','leased','retry_wait') ORDER BY created_at DESC LIMIT 1`, userID, existingID))
		if err != nil {
			return domain.CreatedBodyPresentation{}, err
		}
		op, err := scanOperation(tx.QueryRow(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, task.OperationID))
		if err != nil {
			return domain.CreatedBodyPresentation{}, err
		}
		return domain.CreatedBodyPresentation{Presentation: item, Operation: op, Task: task, Reused: true}, tx.Commit(ctx)
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.CreatedBodyPresentation{}, existingErr
	}

	item, err := scanStoredBodyPresentation(tx.QueryRow(ctx, `
		INSERT INTO body_presentations(user_id,body_media_id,face_media_id)
		VALUES($1::uuid,$2::uuid,$3::uuid)
		RETURNING id::text,body_media_id::text,face_media_id::text,representation,video_storage_key,duration_ms,frames,mesh,provider_version,created_at,updated_at`,
		userID, input.BodyMediaID, input.FaceMediaID))
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}

	var opID string
	if err = tx.QueryRow(ctx, `
		INSERT INTO operations(user_id,kind,subject_type,subject_id,status,stage_code)
		VALUES ($1::uuid,'body_orbit','body_presentation',$2::uuid,'accepted','')
		RETURNING id::text`, userID, item.Presentation.ID).Scan(&opID); err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	op, err := scanOperation(tx.QueryRow(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, opID))
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}

	payload, err := json.Marshal(domain.BodyOrbitTaskPayload{PresentationID: item.Presentation.ID})
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	task, err := scanBodyOrbitTask(tx.QueryRow(ctx, `
		INSERT INTO tasks(
			user_id,operation_id,type,subject_type,subject_id,subject_generation,
			payload_version,payload,dedupe_key,status,max_attempts,stage_code
		) VALUES (
			$1::uuid,$2::uuid,'body_orbit','body_presentation',$3::uuid,0,
			1,$4,$5,'queued',$6,''
		) RETURNING`+bodyOrbitTaskReturning,
		userID, op.ID, item.Presentation.ID, payload, "body_orbit:"+item.Presentation.ID, maxAttempts))
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	return domain.CreatedBodyPresentation{Presentation: item, Operation: op, Task: task}, tx.Commit(ctx)
}

func (s *Store) GetBodyPresentation(ctx context.Context, userID, id string) (domain.StoredBodyPresentation, error) {
	item, err := scanStoredBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+` WHERE id=$1::uuid AND user_id=$2::uuid`, id, userID))
	return item, mapNotFound(err)
}

// GetBodyOrbitWork 是 worker 取件：只需输入媒体 ID。
func (s *Store) GetBodyOrbitWork(ctx context.Context, userID, id string) (domain.BodyPresentationInput, error) {
	var input domain.BodyPresentationInput
	err := s.pool.QueryRow(ctx, `SELECT body_media_id::text, face_media_id::text FROM body_presentations WHERE id=$1::uuid AND user_id=$2::uuid`, id, userID).
		Scan(&input.BodyMediaID, &input.FaceMediaID)
	return input, mapNotFound(err)
}

// ListBodyPresentationStatus 取实验室启动读模型：进行中 / 最新可播成功 /
// 比成功更新的最新失败（无则各自为 nil）。
func (s *Store) ListBodyPresentationStatus(ctx context.Context, userID string) (active, completed, failed *domain.StoredBodyPresentation, err error) {
	completedItem, completedErr := scanStoredBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+`
		WHERE user_id=$1::uuid AND video_storage_key <> ''
		ORDER BY created_at DESC LIMIT 1`, userID))
	if completedErr == nil {
		completed = &completedItem
	} else if !errors.Is(completedErr, pgx.ErrNoRows) {
		return nil, nil, nil, completedErr
	}

	activeItem, activeErr := scanStoredBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+`
		WHERE id IN (
			SELECT p.id FROM body_presentations p
			JOIN tasks t ON t.type='body_orbit' AND t.subject_id=p.id AND t.user_id=p.user_id
			WHERE p.user_id=$1::uuid AND t.status IN ('queued','leased','retry_wait')
			ORDER BY t.created_at DESC LIMIT 1
		)`, userID))
	if activeErr == nil {
		active = &activeItem
	} else if !errors.Is(activeErr, pgx.ErrNoRows) {
		return nil, nil, nil, activeErr
	}

	failedQuery := bodyPresentationSelect + `
		WHERE id IN (
			SELECT p.id FROM body_presentations p
			JOIN tasks t ON t.type='body_orbit' AND t.subject_id=p.id AND t.user_id=p.user_id
			WHERE p.user_id=$1::uuid AND t.status='failed'`
	args := []any{userID}
	if completed != nil {
		failedQuery += ` AND t.created_at > $2`
		args = append(args, completed.Presentation.CreatedAt)
	}
	failedQuery += `
			ORDER BY t.created_at DESC LIMIT 1
		)`
	failedItem, failedErr := scanStoredBodyPresentation(s.pool.QueryRow(ctx, failedQuery, args...))
	if failedErr == nil {
		failed = &failedItem
	} else if !errors.Is(failedErr, pgx.ErrNoRows) {
		return nil, nil, nil, failedErr
	}
	return active, completed, failed, nil
}

// BodyOrbitTaskState 投影任务的公开状态：queued/processing/completed/failed。
func (s *Store) BodyOrbitTaskState(ctx context.Context, userID, presentationID string) (domain.Task, error) {
	task, err := scanBodyOrbitTask(s.pool.QueryRow(ctx, bodyOrbitTaskSelect+`
		WHERE user_id=$1::uuid AND subject_id=$2::uuid AND type='body_orbit'
		ORDER BY created_at DESC LIMIT 1`, userID, presentationID))
	return task, mapNotFound(err)
}

// ApplyBodyOrbitResult 由 worker 提交成功结果：只写 object key 与帧键。
func (s *Store) ApplyBodyOrbitResult(ctx context.Context, id, videoKey string, durationMS int, frameYaws []float64, frameKeys []string, providerVersion string) error {
	stored := make([]storedOrbitFrame, len(frameYaws))
	for i, yaw := range frameYaws {
		key := ""
		if i < len(frameKeys) {
			key = frameKeys[i]
		}
		stored[i] = storedOrbitFrame{Yaw: yaw, StorageKey: key}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE body_presentations
		SET video_storage_key=$2,duration_ms=$3,frames=$4,provider_version=$5,updated_at=now()
		WHERE id=$1::uuid`, id, videoKey, durationMS, raw, providerVersion)
	if err == nil && tag.RowsAffected() == 0 {
		return mapNotFound(pgx.ErrNoRows)
	}
	return err
}

const bodyOrbitTaskReturning = `
	id::text, user_id::text, operation_id::text, type, subject_type, subject_id::text,
	subject_generation, payload_version, payload, dedupe_key, status, priority,
	attempt, max_attempts, available_at, progress_bps, stage_code,
	error_class, error_code, created_at, updated_at, finished_at`

const bodyOrbitTaskSelect = `SELECT` + bodyOrbitTaskReturning + ` FROM tasks`

func scanBodyOrbitTask(row pgx.Row) (domain.Task, error) {
	var task domain.Task
	var payload []byte
	var errorClass, errorCode *string
	err := row.Scan(
		&task.ID, &task.UserID, &task.OperationID, &task.Type, &task.SubjectType, &task.SubjectID,
		&task.SubjectGeneration, &task.PayloadVersion, &payload, &task.DedupeKey, &task.Status, &task.Priority,
		&task.Attempt, &task.MaxAttempts, &task.AvailableAt, &task.ProgressBPS, &task.StageCode,
		&errorClass, &errorCode, &task.CreatedAt, &task.UpdatedAt, &task.FinishedAt)
	if err != nil {
		return task, err
	}
	task.Payload = payload
	if errorClass != nil {
		task.ErrorClass = domain.ErrorClass(*errorClass)
	}
	if errorCode != nil {
		task.ErrorCode = *errorCode
	}
	return task, nil
}

// CommitBodyOrbit 在租约校验下收尾：成功翻任务/操作终态，DomainFail 记失败。
// 结果数据（视频/帧键）已在 Execute 阶段写入 body_presentations。
func (s *Store) CommitBodyOrbit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var operationID string
	err = tx.QueryRow(ctx, `
		SELECT operation_id::text
		FROM tasks
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()
		FOR UPDATE`, lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner).
		Scan(&operationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CommitSuperseded, nil
	}
	if err != nil {
		return "", err
	}

	if result.Disposition == domain.TaskDomainFail {
		code := ""
		if result.Failure != nil {
			code = result.Failure.Code
		}
		if _, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status='failed', lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
			    error_class='permanent', error_code=$3, finished_at=now(), updated_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID, code); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE operations
			SET status='failed', error_code=$3, finished_at=now(), updated_at=now(), version=version+1
			WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, lease.UserID, code); err != nil {
			return "", err
		}
		return domain.CommitApplied, tx.Commit(ctx)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status='succeeded', lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
		    progress_bps=10000, finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()`,
		lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return domain.CommitSuperseded, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations
		SET status='succeeded', result_type='body_presentation', result_id=$3::uuid,
		    progress_bps=10000, finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, lease.UserID, result.ResultID); err != nil {
		return "", err
	}
	return domain.CommitApplied, tx.Commit(ctx)
}
