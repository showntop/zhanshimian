package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zhanshimian/server/internal/domain"
)

// generationFeedbackRow 是 generation_feedback 的完整行,包含 domain.GenerationFeedback
// 投影刻意省略的内部字段(user_id、idempotency_key、request_hash)。
type generationFeedbackRow struct {
	ID             string
	UserID         string
	PublicationID  string
	RenderRunID    string
	CandidateID    string
	AssetID        string
	Generation     int
	Tags           []string
	Comment        string
	MediaAssetID   *string
	IdempotencyKey string
	RequestHash    string
	CreatedAt      time.Time
}

func (r generationFeedbackRow) public() domain.GenerationFeedback {
	tags := make([]domain.Tag, 0, len(r.Tags))
	for _, t := range r.Tags {
		tags = append(tags, domain.Tag(t))
	}
	return domain.GenerationFeedback{
		ID:            r.ID,
		PublicationID: r.PublicationID,
		RenderRunID:   r.RenderRunID,
		CandidateID:   r.CandidateID,
		AssetID:       r.AssetID,
		Generation:    r.Generation,
		Tags:          tags,
		Comment:       r.Comment,
		MediaAssetID:  r.MediaAssetID,
		CreatedAt:     r.CreatedAt,
	}
}

const generationFeedbackSelect = `
	SELECT id::text, user_id::text, publication_id::text, render_run_id::text, candidate_id::text,
	       asset_id::text, generation, tags, comment, media_asset_id::text, idempotency_key, request_hash, created_at
	FROM generation_feedback`

const generationFeedbackReturning = `
	RETURNING id::text, user_id::text, publication_id::text, render_run_id::text, candidate_id::text,
	          asset_id::text, generation, tags, comment, media_asset_id::text, idempotency_key, request_hash, created_at`

func scanGenerationFeedbackRow(row rowScanner) (generationFeedbackRow, error) {
	var r generationFeedbackRow
	err := row.Scan(
		&r.ID, &r.UserID, &r.PublicationID, &r.RenderRunID, &r.CandidateID,
		&r.AssetID, &r.Generation, &r.Tags, &r.Comment, &r.MediaAssetID,
		&r.IdempotencyKey, &r.RequestHash, &r.CreatedAt,
	)
	return r, err
}

// generationChain 是 publication join candidate 派生的完整发布链路。
type generationChain struct {
	RenderRunID string
	CandidateID string
	AssetID     string
	Generation  int
}

// CreateGenerationFeedback 在单个事务里锁定 publication 串行化并发提交、做幂等
// 重放/冲突,再从 publication join candidate 派生链路、校验可选反馈图片,最后插入。
func (s *Store) CreateGenerationFeedback(ctx context.Context, command domain.CreateGenerationFeedbackCommand) (domain.GenerationFeedback, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.GenerationFeedback{}, false, err
	}
	defer tx.Rollback(ctx)

	chain, err := lockGenerationPublication(ctx, tx, command.UserID, command.PublicationID)
	if err != nil {
		return domain.GenerationFeedback{}, false, err
	}

	if existing, found, err := findGenerationFeedbackByKey(ctx, tx, command.UserID, command.IdempotencyKey); err != nil {
		return domain.GenerationFeedback{}, false, err
	} else if found {
		if existing.RequestHash != command.RequestHash {
			return domain.GenerationFeedback{}, false, domain.ErrIdempotencyConflict
		}
		return existing.public(), false, nil
	}

	if command.MediaAssetID != nil {
		if err := verifyFeedbackMedia(ctx, tx, command.UserID, *command.MediaAssetID); err != nil {
			return domain.GenerationFeedback{}, false, err
		}
	}

	row, err := insertGenerationFeedback(ctx, tx, command, chain)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, found, lookupErr := findGenerationFeedbackByKey(ctx, tx, command.UserID, command.IdempotencyKey); lookupErr != nil {
				return domain.GenerationFeedback{}, false, lookupErr
			} else if found {
				if existing.RequestHash != command.RequestHash {
					return domain.GenerationFeedback{}, false, domain.ErrIdempotencyConflict
				}
				return existing.public(), false, nil
			}
		}
		return domain.GenerationFeedback{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.GenerationFeedback{}, false, err
	}
	return row.public(), true, nil
}

// lockGenerationPublication 锁定 publication 并从 render_candidates join 出
// asset_id;跨用户、已删除或不存在的 publication 一律读作不存在。
func lockGenerationPublication(ctx context.Context, tx pgx.Tx, userID, publicationID string) (generationChain, error) {
	var chain generationChain
	err := tx.QueryRow(ctx, `
		SELECT p.render_run_id::text, p.candidate_id::text, c.asset_id::text, p.generation
		FROM render_publications p
		JOIN render_candidates c ON c.user_id = p.user_id AND c.id = p.candidate_id
		WHERE p.user_id = $1::uuid AND p.id = $2::uuid
		FOR UPDATE OF p`,
		userID, publicationID).Scan(&chain.RenderRunID, &chain.CandidateID, &chain.AssetID, &chain.Generation)
	if err != nil {
		return generationChain{}, mapNotFound(err)
	}
	return chain, nil
}

func findGenerationFeedbackByKey(ctx context.Context, tx pgx.Tx, userID, key string) (generationFeedbackRow, bool, error) {
	row, err := scanGenerationFeedbackRow(tx.QueryRow(ctx, generationFeedbackSelect+` WHERE user_id=$1::uuid AND idempotency_key=$2`, userID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return generationFeedbackRow{}, false, nil
	}
	if err != nil {
		return generationFeedbackRow{}, false, err
	}
	return row, true, nil
}

// verifyFeedbackMedia 确认可选反馈图片属于该用户、目的为 feedback、状态 ready
// 且为 JPEG/PNG;三者任一不满足都读作不存在。
func verifyFeedbackMedia(ctx context.Context, tx pgx.Tx, userID, mediaAssetID string) error {
	var one int
	err := tx.QueryRow(ctx, `
		SELECT 1 FROM media_assets
		WHERE user_id=$1::uuid AND id=$2::uuid AND deleted_at IS NULL
		  AND purpose='feedback' AND state='ready' AND mime_type IN ('image/jpeg','image/png')`,
		userID, mediaAssetID).Scan(&one)
	return mapNotFound(err)
}

func insertGenerationFeedback(ctx context.Context, tx pgx.Tx, command domain.CreateGenerationFeedbackCommand, chain generationChain) (generationFeedbackRow, error) {
	mediaAssetID := any(nil)
	if command.MediaAssetID != nil {
		mediaAssetID = *command.MediaAssetID
	}
	tags := make([]string, len(command.Tags))
	for i, t := range command.Tags {
		tags[i] = string(t)
	}
	return scanGenerationFeedbackRow(tx.QueryRow(ctx, `
		INSERT INTO generation_feedback(user_id, publication_id, render_run_id, candidate_id, asset_id, generation, tags, comment, media_asset_id, idempotency_key, request_hash)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8, $9::uuid, $10, $11)
		`+generationFeedbackReturning,
		command.UserID, command.PublicationID, chain.RenderRunID, chain.CandidateID, chain.AssetID, chain.Generation,
		tags, command.Comment, mediaAssetID, command.IdempotencyKey, command.RequestHash))
}
