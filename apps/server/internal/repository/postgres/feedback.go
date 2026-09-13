package postgres

import (
	"context"
	"encoding/json"
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

// executionFeedbackRow 是 execution_feedback 的完整行,包含 domain.ExecutionFeedback
// 投影刻意省略的内部字段(user_id、idempotency_key、request_hash)。
type executionFeedbackRow struct {
	ID             string
	UserID         string
	ExecutionID    string
	SelectionID    string
	PlanSetID      string
	Tags           []string
	Comment        string
	MediaAssetID   *string
	IdempotencyKey string
	RequestHash    string
	CreatedAt      time.Time
}

func (r executionFeedbackRow) public() domain.ExecutionFeedback {
	tags := make([]domain.Tag, 0, len(r.Tags))
	for _, t := range r.Tags {
		tags = append(tags, domain.Tag(t))
	}
	return domain.ExecutionFeedback{
		ID:           r.ID,
		ExecutionID:  r.ExecutionID,
		SelectionID:  r.SelectionID,
		PlanSetID:    r.PlanSetID,
		Tags:         tags,
		Comment:      r.Comment,
		MediaAssetID: r.MediaAssetID,
		CreatedAt:    r.CreatedAt,
	}
}

const executionFeedbackSelect = `
	SELECT id::text, user_id::text, execution_id::text, selection_id::text, plan_set_id::text,
	       tags, comment, media_asset_id::text, idempotency_key, request_hash, created_at
	FROM execution_feedback`

const executionFeedbackReturning = `
	RETURNING id::text, user_id::text, execution_id::text, selection_id::text, plan_set_id::text,
	          tags, comment, media_asset_id::text, idempotency_key, request_hash, created_at`

func scanExecutionFeedbackRow(row rowScanner) (executionFeedbackRow, error) {
	var r executionFeedbackRow
	err := row.Scan(
		&r.ID, &r.UserID, &r.ExecutionID, &r.SelectionID, &r.PlanSetID,
		&r.Tags, &r.Comment, &r.MediaAssetID, &r.IdempotencyKey, &r.RequestHash, &r.CreatedAt,
	)
	return r, err
}

// preferenceMemoryRow 是 preference_memories 的完整行。
type preferenceMemoryRow struct {
	ID                  string
	UserID              string
	ExecutionFeedbackID string
	Key                 string
	Category            string
	Value               string
	SourceTag           string
	CreatedAt           time.Time
}

func (r preferenceMemoryRow) public() domain.PreferenceMemory {
	return domain.PreferenceMemory{
		ID:        r.ID,
		Key:       r.Key,
		Category:  domain.PreferenceCategory(r.Category),
		Value:     r.Value,
		SourceTag: domain.Tag(r.SourceTag),
		CreatedAt: r.CreatedAt,
	}
}

const preferenceMemorySelect = `
	SELECT id::text, user_id::text, execution_feedback_id::text, memory_key, category, value, source_tag, created_at
	FROM preference_memories`

const preferenceMemoryReturning = `
	RETURNING id::text, user_id::text, execution_feedback_id::text, memory_key, category, value, source_tag, created_at`

func scanPreferenceMemoryRow(row rowScanner) (preferenceMemoryRow, error) {
	var r preferenceMemoryRow
	err := row.Scan(
		&r.ID, &r.UserID, &r.ExecutionFeedbackID, &r.Key, &r.Category, &r.Value, &r.SourceTag, &r.CreatedAt,
	)
	return r, err
}

// CreateExecutionFeedback 在单个事务里锁定 Execution 并要求 completed、从 selection
// join plan_set 派生链路、校验可选反馈图片、做幂等重放/冲突、插入 ExecutionFeedback
// 与 NormalizePreference 产出的 0 或 1 条 PreferenceMemory,最后以 CAS 更新
// user_profiles.preferences 投影。
func (s *Store) CreateExecutionFeedback(ctx context.Context, command domain.CreateExecutionFeedbackCommand) (domain.ExecutionFeedback, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ExecutionFeedback{}, false, err
	}
	defer tx.Rollback(ctx)

	exec, err := lockExecution(ctx, tx, command.UserID, command.ExecutionID)
	if err != nil {
		return domain.ExecutionFeedback{}, false, err
	}
	if exec.State != string(domain.ExecutionCompleted) {
		return domain.ExecutionFeedback{}, false, domain.ErrExecutionNotCompleted
	}

	planSetID, err := lockExecutionSelection(ctx, tx, command.UserID, exec.SelectionID)
	if err != nil {
		return domain.ExecutionFeedback{}, false, err
	}

	if existing, found, err := findExecutionFeedbackByKey(ctx, tx, command.UserID, command.IdempotencyKey); err != nil {
		return domain.ExecutionFeedback{}, false, err
	} else if found {
		if existing.RequestHash != command.RequestHash {
			return domain.ExecutionFeedback{}, false, domain.ErrIdempotencyConflict
		}
		memories, err := listPreferenceMemoriesByFeedback(ctx, tx, command.UserID, existing.ID)
		if err != nil {
			return domain.ExecutionFeedback{}, false, err
		}
		fb := existing.public()
		fb.AppliedMemories = memories
		return fb, false, nil
	}

	if command.MediaAssetID != nil {
		if err := verifyFeedbackMedia(ctx, tx, command.UserID, *command.MediaAssetID); err != nil {
			return domain.ExecutionFeedback{}, false, err
		}
	}

	drafts, err := domain.NormalizePreference(command.Preference, command.Tags)
	if err != nil {
		return domain.ExecutionFeedback{}, false, err
	}

	row, err := insertExecutionFeedback(ctx, tx, command, exec.SelectionID, planSetID)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, found, lookupErr := findExecutionFeedbackByKey(ctx, tx, command.UserID, command.IdempotencyKey); lookupErr != nil {
				return domain.ExecutionFeedback{}, false, lookupErr
			} else if found {
				if existing.RequestHash != command.RequestHash {
					return domain.ExecutionFeedback{}, false, domain.ErrIdempotencyConflict
				}
				memories, listErr := listPreferenceMemoriesByFeedback(ctx, tx, command.UserID, existing.ID)
				if listErr != nil {
					return domain.ExecutionFeedback{}, false, listErr
				}
				fb := existing.public()
				fb.AppliedMemories = memories
				return fb, false, nil
			}
		}
		return domain.ExecutionFeedback{}, false, err
	}

	memories, err := insertPreferenceMemories(ctx, tx, command.UserID, row.ID, drafts)
	if err != nil {
		return domain.ExecutionFeedback{}, false, err
	}

	if len(memories) > 0 {
		if err := applyPreferenceMemoryProjection(ctx, tx, command.UserID); err != nil {
			return domain.ExecutionFeedback{}, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ExecutionFeedback{}, false, err
	}

	fb := row.public()
	fb.AppliedMemories = memories
	return fb, true, nil
}

// lockExecutionSelection 锁定 selection 并派生其 plan_set_id;跨用户、不存在的
// selection 一律读作不存在。
func lockExecutionSelection(ctx context.Context, tx pgx.Tx, userID, selectionID string) (string, error) {
	var planSetID string
	err := tx.QueryRow(ctx, `
		SELECT plan_set_id::text FROM plan_selections
		WHERE user_id=$1::uuid AND id=$2::uuid
		FOR UPDATE`, userID, selectionID).Scan(&planSetID)
	if err != nil {
		return "", mapNotFound(err)
	}
	return planSetID, nil
}

func findExecutionFeedbackByKey(ctx context.Context, tx pgx.Tx, userID, key string) (executionFeedbackRow, bool, error) {
	row, err := scanExecutionFeedbackRow(tx.QueryRow(ctx, executionFeedbackSelect+` WHERE user_id=$1::uuid AND idempotency_key=$2`, userID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return executionFeedbackRow{}, false, nil
	}
	if err != nil {
		return executionFeedbackRow{}, false, err
	}
	return row, true, nil
}

func insertExecutionFeedback(ctx context.Context, tx pgx.Tx, command domain.CreateExecutionFeedbackCommand, selectionID, planSetID string) (executionFeedbackRow, error) {
	mediaAssetID := any(nil)
	if command.MediaAssetID != nil {
		mediaAssetID = *command.MediaAssetID
	}
	tags := make([]string, len(command.Tags))
	for i, t := range command.Tags {
		tags[i] = string(t)
	}
	pref, err := json.Marshal(command.Preference)
	if err != nil {
		return executionFeedbackRow{}, err
	}
	return scanExecutionFeedbackRow(tx.QueryRow(ctx, `
		INSERT INTO execution_feedback(user_id, execution_id, selection_id, plan_set_id, tags, comment, media_asset_id, preference, idempotency_key, request_hash)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7::uuid, $8::jsonb, $9, $10)
		`+executionFeedbackReturning,
		command.UserID, command.ExecutionID, selectionID, planSetID, tags, command.Comment, mediaAssetID, pref, command.IdempotencyKey, command.RequestHash))
}

func insertPreferenceMemories(ctx context.Context, tx pgx.Tx, userID, executionFeedbackID string, drafts []domain.PreferenceMemoryDraft) ([]domain.PreferenceMemory, error) {
	memories := make([]domain.PreferenceMemory, 0, len(drafts))
	for _, d := range drafts {
		row, err := scanPreferenceMemoryRow(tx.QueryRow(ctx, `
			INSERT INTO preference_memories(user_id, execution_feedback_id, memory_key, category, value, source_tag)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
			`+preferenceMemoryReturning,
			userID, executionFeedbackID, d.Key, string(d.Category), d.Value, string(d.SourceTag)))
		if err != nil {
			return nil, err
		}
		memories = append(memories, row.public())
	}
	return memories, nil
}

// listPreferenceMemoriesByFeedback 读取单条 ExecutionFeedback 已写入的记忆。
func listPreferenceMemoriesByFeedback(ctx context.Context, q executionQuerier, userID, executionFeedbackID string) ([]domain.PreferenceMemory, error) {
	rows, err := q.Query(ctx, preferenceMemorySelect+`
		WHERE user_id=$1::uuid AND execution_feedback_id=$2::uuid
		ORDER BY created_at DESC, id DESC`, userID, executionFeedbackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PreferenceMemory
	for rows.Next() {
		row, err := scanPreferenceMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row.public())
	}
	return out, rows.Err()
}

// ListPreferenceMemories 按最近写入顺序返回该用户的偏好记忆,供下一次 Planning 消费。
func (s *Store) ListPreferenceMemories(ctx context.Context, userID string, limit int) ([]domain.PreferenceMemory, error) {
	rows, err := s.pool.Query(ctx, preferenceMemorySelect+`
		WHERE user_id=$1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PreferenceMemory
	for rows.Next() {
		row, err := scanPreferenceMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row.public())
	}
	return out, rows.Err()
}

// feedbackMemoryProjection 是 user_profiles.preferences.feedback_memory 的确定
// 读优化投影;数据库事实以 preference_memories 为准。
type feedbackMemoryProjection struct {
	Formality   string             `json:"formality,omitempty"`
	Complexity  string             `json:"complexity,omitempty"`
	AvoidColors []string           `json:"avoid_colors,omitempty"`
	Preserve    preserveProjection `json:"preserve"`
}

type preserveProjection struct {
	Hair   []string `json:"hair"`
	Makeup []string `json:"makeup"`
	Outfit []string `json:"outfit"`
}

// applyPreferenceMemoryProjection 锁定 profile 行串行化并发反馈,从
// preference_memories 事实源重建 feedback_memory 投影(formality/complexity
// 单值最近写入优先;avoid_colors/preserve_* 数组去重、每类最多 10 项、保留最近
// 顺序),再以 version=version+1 的 CAS 写回 preferences。
func applyPreferenceMemoryProjection(ctx context.Context, tx pgx.Tx, userID string) error {
	var prefs []byte
	if err := tx.QueryRow(ctx, `SELECT preferences FROM user_profiles WHERE user_id=$1::uuid FOR UPDATE`, userID).Scan(&prefs); err != nil {
		return mapNotFound(err)
	}

	rows, err := tx.Query(ctx, `
		SELECT memory_key, value FROM preference_memories
		WHERE user_id=$1::uuid
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	proj := feedbackMemoryProjection{}
	seenAvoid := map[string]bool{}
	seenPreserve := map[string]map[string]bool{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		switch key {
		case "formality":
			if proj.Formality == "" {
				proj.Formality = value
			}
		case "complexity":
			if proj.Complexity == "" {
				proj.Complexity = value
			}
		case "avoid_color":
			if !seenAvoid[value] && len(proj.AvoidColors) < 10 {
				proj.AvoidColors = append(proj.AvoidColors, value)
				seenAvoid[value] = true
			}
		case "preserve_hair", "preserve_makeup", "preserve_outfit":
			target := appendPreserveTarget(&proj.Preserve, key)
			bucket := seenPreserve[key]
			if bucket == nil {
				bucket = map[string]bool{}
				seenPreserve[key] = bucket
			}
			if !bucket[value] && len(*target) < 10 {
				*target = append(*target, value)
				bucket[value] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	root := map[string]json.RawMessage{}
	if len(prefs) > 0 {
		if err := json.Unmarshal(prefs, &root); err != nil {
			return err
		}
	}
	fm, err := json.Marshal(proj)
	if err != nil {
		return err
	}
	root["feedback_memory"] = fm
	merged, err := json.Marshal(root)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE user_profiles SET preferences = $2::jsonb, version = version + 1, updated_at = now()
		WHERE user_id=$1::uuid`, userID, merged)
	return err
}

func appendPreserveTarget(p *preserveProjection, key string) *[]string {
	switch key {
	case "preserve_hair":
		return &p.Hair
	case "preserve_makeup":
		return &p.Makeup
	default:
		return &p.Outfit
	}
}
