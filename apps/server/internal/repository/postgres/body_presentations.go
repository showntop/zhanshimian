package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// storedOrbitFrame is the jsonb shape persisted on body_presentations.frames.
// OrbitFrame for the API drops storage_key on read.
type storedOrbitFrame struct {
	Yaw        float64 `json:"yaw"`
	URL        string  `json:"url"`
	StorageKey string  `json:"storage_key"`
}

const bodyPresentationSelect = `SELECT id::text,body_media_id::text,face_media_id::text,representation,video_url,duration_ms,frames,mesh,provider_version,created_at,updated_at FROM body_presentations`

func mapOrbitFrames(raw []byte) ([]domain.OrbitFrame, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []domain.OrbitFrame{}, nil
	}
	var stored []storedOrbitFrame
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	frames := make([]domain.OrbitFrame, 0, len(stored))
	for _, frame := range stored {
		frames = append(frames, domain.OrbitFrame{Yaw: frame.Yaw, URL: frame.URL})
	}
	return frames, nil
}

func scanBodyPresentation(row pgx.Row) (domain.BodyPresentation, error) {
	var item domain.BodyPresentation
	var framesRaw, meshRaw []byte
	err := row.Scan(
		&item.ID,
		&item.BodyMediaID,
		&item.FaceMediaID,
		&item.Representation,
		&item.Orbit.VideoURL,
		&item.Orbit.DurationMS,
		&framesRaw,
		&meshRaw,
		&item.ProviderVersion,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	frames, err := mapOrbitFrames(framesRaw)
	if err != nil {
		return item, err
	}
	item.Orbit.Frames = frames
	if len(meshRaw) > 0 && string(meshRaw) != "null" {
		var mesh domain.BodyMesh
		if err := json.Unmarshal(meshRaw, &mesh); err != nil {
			return item, err
		}
		item.Mesh = &mesh
	}
	return item, nil
}

func (s *Store) CreateBodyPresentation(ctx context.Context, userID string, input domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.BodyPresentation{}, nil, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "body-orbit:"+userID); err != nil {
		return domain.BodyPresentation{}, nil, false, err
	}
	var existingID string
	existingErr := tx.QueryRow(ctx, `
		SELECT p.id FROM body_presentations p
		JOIN tasks t ON t.type='body_orbit' AND t.payload->>'presentation_id'=p.id::text
		WHERE p.user_id=$1 AND t.status IN ('queued','processing')
		ORDER BY t.created_at DESC LIMIT 1`, userID).Scan(&existingID)
	if existingErr == nil {
		item, itemErr := scanBodyPresentation(tx.QueryRow(ctx, bodyPresentationSelect+` WHERE id=$1::uuid AND user_id=$2`, existingID, userID))
		if itemErr != nil {
			return domain.BodyPresentation{}, nil, false, itemErr
		}
		task, taskErr := scanTask(tx.QueryRow(ctx, taskSelect+` WHERE type='body_orbit' AND payload->>'presentation_id'=$1 AND status IN ('queued','processing') ORDER BY created_at DESC LIMIT 1`, existingID))
		if taskErr != nil {
			return domain.BodyPresentation{}, nil, false, taskErr
		}
		return item, &task, false, tx.Commit(ctx)
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.BodyPresentation{}, nil, false, existingErr
	}
	item, err := scanBodyPresentation(tx.QueryRow(ctx, `
		INSERT INTO body_presentations(user_id,body_media_id,face_media_id)
		VALUES($1,$2,$3)
		RETURNING id::text,body_media_id::text,face_media_id::text,representation,video_url,duration_ms,frames,mesh,provider_version,created_at,updated_at`,
		userID, input.BodyMediaID, input.FaceMediaID))
	if err != nil {
		return domain.BodyPresentation{}, nil, false, err
	}
	task, err := scanTask(tx.QueryRow(ctx, `
		INSERT INTO tasks(user_id,type,payload,stage)
		VALUES($1,'body_orbit',$2,'正在排队') `+taskSelectTail,
		userID, domain.BodyOrbitTaskPayload{PresentationID: item.ID}))
	if err != nil {
		return domain.BodyPresentation{}, nil, false, err
	}
	return item, &task, true, tx.Commit(ctx)
}

func (s *Store) GetBodyPresentation(ctx context.Context, userID, id string) (domain.BodyPresentation, error) {
	item, err := scanBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+` WHERE id=$1::uuid AND user_id=$2`, id, userID))
	return item, mapNotFound(err)
}

func (s *Store) GetBodyOrbitWork(ctx context.Context, userID, id string) (domain.BodyPresentationInput, error) {
	var input domain.BodyPresentationInput
	err := s.pool.QueryRow(ctx, `SELECT body_media_id::text, face_media_id::text FROM body_presentations WHERE id=$1::uuid AND user_id=$2`, id, userID).
		Scan(&input.BodyMediaID, &input.FaceMediaID)
	return input, mapNotFound(err)
}

func (s *Store) ListBodyPresentationStatus(ctx context.Context, userID string) (active, completed, failed *domain.BodyPresentation, err error) {
	completedItem, completedErr := scanBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+`
		WHERE user_id=$1 AND video_url <> ''
		ORDER BY created_at DESC LIMIT 1`, userID))
	if completedErr == nil {
		completed = &completedItem
	} else if !errors.Is(completedErr, pgx.ErrNoRows) {
		return nil, nil, nil, completedErr
	}

	activeItem, activeErr := scanBodyPresentation(s.pool.QueryRow(ctx, bodyPresentationSelect+`
		WHERE id IN (
			SELECT p.id FROM body_presentations p
			JOIN tasks t ON t.type='body_orbit' AND t.payload->>'presentation_id'=p.id::text
			WHERE p.user_id=$1 AND t.status IN ('queued','processing')
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
			JOIN tasks t ON t.type='body_orbit' AND t.payload->>'presentation_id'=p.id::text
			WHERE p.user_id=$1 AND t.status='failed'`
	args := []any{userID}
	if completed != nil {
		failedQuery += ` AND t.created_at > $2`
		args = append(args, completed.CreatedAt)
	}
	failedQuery += `
			ORDER BY t.created_at DESC LIMIT 1
		)`
	failedItem, failedErr := scanBodyPresentation(s.pool.QueryRow(ctx, failedQuery, args...))
	if failedErr == nil {
		failed = &failedItem
	} else if !errors.Is(failedErr, pgx.ErrNoRows) {
		return nil, nil, nil, failedErr
	}
	return active, completed, failed, nil
}

func (s *Store) ApplyBodyOrbitResult(ctx context.Context, id, videoURL, videoKey string, durationMS int, frames []domain.OrbitFrame, frameKeys []string, providerVersion string) error {
	stored := make([]storedOrbitFrame, len(frames))
	for i, frame := range frames {
		key := ""
		if i < len(frameKeys) {
			key = frameKeys[i]
		}
		stored[i] = storedOrbitFrame{Yaw: frame.Yaw, URL: frame.URL, StorageKey: key}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE body_presentations
		SET video_url=$2,video_storage_key=$3,duration_ms=$4,frames=$5,provider_version=$6,updated_at=now()
		WHERE id=$1::uuid`, id, videoURL, videoKey, durationMS, raw, providerVersion)
	if err == nil && tag.RowsAffected() == 0 {
		return repository.ErrTaskRemoved
	}
	return err
}
