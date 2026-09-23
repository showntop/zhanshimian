package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/hair"
)

var (
	_ hair.Reader      = (*Store)(nil)
	_ hair.Writer      = (*Store)(nil)
	_ hair.RunCreator  = (*Store)(nil)
	_ hair.Hairstyler  = (*Store)(nil)
	_ hair.HandlerRepo = (*Store)(nil)
)

// hairPreviewSelectSQL 是预览读模型：源图与结果图都直挂 media_assets，
// 来源/角标按真实 origin/display_kind 投影（provider_output→generated_preview
// →「风格参考」），object key 仅供读取时即时签名，不存 URL。
const hairPreviewSelectSQL = `
	SELECT hp.id::text, hp.style_id, hp.style_name, hp.state, hp.retryable,
	       COALESCE(op.id::text,''), COALESCE(op.kind::text,''), COALESCE(op.status::text,''),
	       src_ma.id::text,
	       CASE src_ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE src_ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END,
	       COALESCE(src_ma.mime_type,''), COALESCE(src_ma.object_key,''),
	       res_ma.id::text,
	       CASE res_ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE res_ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END,
	       COALESCE(res_ma.mime_type,''), COALESCE(res_ma.object_key,''),
	       hp.saved, hp.created_at, hp.updated_at
	FROM hair_previews hp
	LEFT JOIN operations op ON op.user_id=hp.user_id AND op.id=hp.operation_id
	LEFT JOIN media_assets src_ma ON src_ma.user_id=hp.user_id AND src_ma.id=hp.source_media_asset_id
	LEFT JOIN media_assets res_ma ON res_ma.user_id=hp.user_id AND res_ma.id=hp.result_media_asset_id`

// CreateHairPreviewRun 在单事务内落预览行、公开 Operation（kind=render,
// subject_type=hair_preview）与首个 hair_preview 任务：worker 注册表消费
// 该任务驱动生成，轮询语义与渲染一致。
func (s *Store) CreateHairPreviewRun(ctx context.Context, userID string, params hair.CreateRunParams) (hair.Preview, domain.OperationRef, error) {
	if params.MaxAttempts <= 0 {
		params.MaxAttempts = hair.TaskMaxAttempts
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}
	defer tx.Rollback(ctx)

	preview := hair.Preview{
		StyleID: params.StyleID, StyleName: params.StyleName,
		State: hair.StateQueued,
	}
	if err = tx.QueryRow(ctx, `
		INSERT INTO hair_previews(user_id, style_id, style_name, source_media_asset_id, state)
		VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, 'queued')
		RETURNING id::text, created_at, updated_at`,
		userID, params.StyleID, params.StyleName, params.SourceAssetID).
		Scan(&preview.ID, &preview.CreatedAt, &preview.UpdatedAt); err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}

	operation := domain.OperationRef{Kind: domain.OperationRender, Status: domain.OperationAccepted}
	if err = tx.QueryRow(ctx, `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status, stage_code)
		VALUES ($1::uuid,'render','hair_preview',$2::uuid,'accepted','')
		RETURNING id::text`, userID, preview.ID).Scan(&operation.ID); err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE hair_previews SET operation_id=$3::uuid, updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, preview.ID, operation.ID); err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}

	payload, err := json.Marshal(domain.HairPreviewTaskPayload{PreviewID: preview.ID})
	if err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO tasks(user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES ($1::uuid,$2::uuid,$3,'hair_preview',$4::uuid,0,$5,$6,$7,'queued','',$8)`,
		userID, operation.ID, string(hair.TaskTypeHairPreview), preview.ID,
		hair.TaskPayloadVersion, payload, "hair_preview:"+preview.ID, params.MaxAttempts); err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return hair.Preview{}, domain.OperationRef{}, err
	}
	preview.Operation = operation
	return preview, operation, nil
}

func (s *Store) GetHairPreviewRow(ctx context.Context, userID string, id string) (hair.Preview, error) {
	return s.scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelectSQL+` WHERE hp.user_id=$1::uuid AND hp.id=$2::uuid`, userID, id))
}

func (s *Store) GetInFlightHairPreview(ctx context.Context, userID string) (hair.Preview, error) {
	return s.scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelectSQL+`
		WHERE hp.user_id=$1::uuid AND hp.state IN ('queued','generating','checking')
		ORDER BY hp.created_at DESC LIMIT 1`, userID))
}

// ListHairPreviewRows 按契约返回预览历史（新到旧）：无过滤返回全部（含进行中），
// saved 只留已保存/未保存，Active 只留进行中。
func (s *Store) ListHairPreviewRows(ctx context.Context, userID string, filter hair.ListFilter) ([]hair.Preview, error) {
	query := hairPreviewSelectSQL + ` WHERE hp.user_id=$1::uuid`
	args := []any{userID}
	if filter.Saved != nil {
		args = append(args, *filter.Saved)
		query += fmt.Sprintf(` AND hp.saved=$%d`, len(args))
	}
	if filter.Active {
		query += ` AND hp.state IN ('queued','generating','checking')`
	}
	query += ` ORDER BY hp.updated_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hair.Preview{}
	for rows.Next() {
		preview, err := s.scanHairPreview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, preview)
	}
	return out, rows.Err()
}

func (s *Store) MarkHairPreviewSavedByID(ctx context.Context, userID string, id string) (hair.Preview, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE hair_previews SET saved=true, updated_at=now() WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return hair.Preview{}, err
	}
	if tag.RowsAffected() == 0 {
		return hair.Preview{}, repository.ErrNotFound
	}
	return s.GetHairPreviewRow(ctx, userID, id)
}

// ReadFaceMedia 取用户显式选择的正脸照：归属、可用状态与可展示格式任一
// 不满足都按 NotFound 处理（越权与不存在不可区分）。
func (s *Store) ReadFaceMedia(ctx context.Context, userID string, assetID string) (domain.MediaAsset, error) {
	asset, err := scanMediaAsset(s.pool.QueryRow(ctx, mediaAssetSelect+`
		WHERE user_id=$1::uuid AND id=$2::uuid AND deleted_at IS NULL
		  AND origin IN ('user_upload','demo')
		  AND state IN ('ready','published')
		  AND mime_type IN ('image/jpeg','image/png')`, userID, assetID))
	return asset, mapNotFound(err)
}

// ---- worker 取件与结果落库 ----

// GetHairPreviewWork 是 worker 取件：预览状态 + 发型 + 源图对象引用。
func (s *Store) GetHairPreviewWork(ctx context.Context, userID string, previewID string) (hair.PreviewWork, error) {
	var work hair.PreviewWork
	err := s.pool.QueryRow(ctx, `
		SELECT hp.state, hp.style_id, hp.style_name,
		       COALESCE(ma.object_key,''), COALESCE(ma.mime_type,'')
		FROM hair_previews hp
		LEFT JOIN media_assets ma ON ma.user_id=hp.user_id AND ma.id=hp.source_media_asset_id
		WHERE hp.user_id=$1::uuid AND hp.id=$2::uuid`, userID, previewID).
		Scan(&work.State, &work.StyleID, &work.StyleName, &work.SourceObjectKey, &work.SourceMIMEType)
	return work, mapNotFound(err)
}

// MarkHairPreviewGenerating 把进行中的预览推进到 generating（幂等）。
func (s *Store) MarkHairPreviewGenerating(ctx context.Context, userID string, previewID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE hair_previews SET state='generating', updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid AND state IN ('queued','generating')`, userID, previewID)
	return err
}

// ApplyHairPreviewResult 落生成结果：provider_output 媒体资产（published,
// generated_reference→「风格参考」角标）+ 预览置 ready。仅在进行中状态生效。
func (s *Store) ApplyHairPreviewResult(ctx context.Context, userID string, previewID string, result hair.AppliedResult) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	assetID := uuid.NewString()
	if _, err = tx.Exec(ctx, `
		INSERT INTO media_assets(id, user_id, origin, purpose, object_key, sha256, mime_type, byte_size, width, height, state, display_kind, provider_invocation_id)
		VALUES ($1::uuid,$2::uuid,'provider_output','render_candidate',$3,$4,$5,$6,$7,$8,'published','generated_reference',NULLIF($9,'')::uuid)`,
		assetID, userID, result.ObjectKey, result.SHA256, result.MIMEType,
		result.ByteSize, result.Width, result.Height, result.ProviderInvocationID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE hair_previews
		SET result_media_asset_id=$3::uuid, state='ready', retryable=false, updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid AND state IN ('queued','generating','checking')`,
		userID, previewID, assetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrConflict
	}
	return tx.Commit(ctx)
}

// CommitHairPreview 在租约 CAS 下收尾：成功翻任务/操作终态（预览已在
// ApplyHairPreviewResult 置 ready）；DomainFail 记任务/操作失败并把预览置
// failed，公开文案与重试标记随失败分类入库。
func (s *Store) CommitHairPreview(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

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
		failure := domain.TaskFailure{}
		if result.Failure != nil {
			failure = *result.Failure
		}
		if _, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status='failed', error_class=NULLIF($3,''), error_code=NULLIF($4,''),
			    lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
			    finished_at=now(), updated_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID, string(failure.Class), failure.Code); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE operations
			SET status='failed', error_code=NULLIF($3,''), trace_id=COALESCE(trace_id,$4),
			    public_message=$5, retryable=$6,
			    finished_at=now(), updated_at=now(), version=version+1
			WHERE id=$1::uuid AND user_id=$2::uuid`,
			operationID, lease.UserID, failure.Code, uuid.NewString(), failure.PublicMessage, failure.Retryable); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE hair_previews SET state='failed', retryable=$3, updated_at=now()
			WHERE user_id=$1::uuid AND id=$2::uuid`, lease.UserID, lease.Task.SubjectID, failure.Retryable); err != nil {
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
		SET status='succeeded', result_type='hair_preview', result_id=$3::uuid,
		    progress_bps=10000, finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, lease.UserID, result.ResultID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE hair_previews SET state='ready', updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, lease.UserID, lease.Task.SubjectID); err != nil {
		return "", err
	}
	return domain.CommitApplied, tx.Commit(ctx)
}

func (s *Store) scanHairPreview(row rowScanner) (hair.Preview, error) {
	var preview hair.Preview
	var opID, opKind, opStatus *string
	var srcAssetID, srcSourceKind, srcDisplayLabel, srcMIME, srcObjectKey *string
	var resAssetID, resSourceKind, resDisplayLabel, resMIME, resObjectKey *string
	err := row.Scan(&preview.ID, &preview.StyleID, &preview.StyleName, &preview.State, &preview.Retryable,
		&opID, &opKind, &opStatus,
		&srcAssetID, &srcSourceKind, &srcDisplayLabel, &srcMIME, &srcObjectKey,
		&resAssetID, &resSourceKind, &resDisplayLabel, &resMIME, &resObjectKey,
		&preview.Saved, &preview.CreatedAt, &preview.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return preview, repository.ErrNotFound
	}
	if err != nil {
		return preview, err
	}
	if opID != nil && *opID != "" {
		preview.Operation = domain.OperationRef{ID: *opID, Kind: domain.OperationKind(*opKind), Status: domain.OperationStatus(*opStatus)}
	}
	if srcAssetID != nil && *srcAssetID != "" {
		preview.SourceMedia = &domain.RenderMediaView{
			AssetID: *srcAssetID, MIMEType: deref(srcMIME),
			SourceKind: deref(srcSourceKind), DisplayLabel: deref(srcDisplayLabel),
		}
		preview.SourceObjectKey = deref(srcObjectKey)
	}
	if resAssetID != nil && *resAssetID != "" {
		preview.Media = &domain.RenderMediaView{
			AssetID: *resAssetID, MIMEType: deref(resMIME),
			SourceKind: deref(resSourceKind), DisplayLabel: deref(resDisplayLabel),
		}
		preview.ResultObjectKey = deref(resObjectKey)
	}
	return preview, nil
}

// ListHairstyles 返回内置发型目录（稳定的 id/name/media；reason 供文案展示）。
// 推荐 AI 能力接入前的确定性实现：目录本身即产品事实，不做个性化排序伪装。
func (s *Store) RecommendHairstyles(ctx context.Context, userID string) ([]domain.HairStyle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ma.id::text, ma.mime_type, ma.object_key,
		       CASE ma.origin
		         WHEN 'user_upload' THEN 'user_original'
		         WHEN 'provider_output' THEN 'generated_preview'
		         WHEN 'bundled_reference' THEN 'bundled_reference'
		         WHEN 'demo' THEN 'demo_example'
		       END,
		       CASE ma.display_kind
		         WHEN 'original' THEN '原本'
		         WHEN 'generated_reference' THEN '风格参考'
		         WHEN 'effect_example' THEN '效果示例'
		         WHEN 'style_reference' THEN '风格参考'
		       END,
		       h.style_key, h.style_name, COALESCE(h.reason,'')
		FROM hairstyles h
		JOIN media_assets ma ON ma.id = h.media_asset_id
		WHERE ma.deleted_at IS NULL
		ORDER BY h.display_order`)
	if errors.Is(err, pgx.ErrNoRows) {
		return []domain.HairStyle{}, nil
	}
	if err != nil {
		// hairstyles 目录表在当前 baseline 中不存在：返回空目录而非失败。
		return []domain.HairStyle{}, nil
	}
	defer rows.Close()
	out := []domain.HairStyle{}
	for rows.Next() {
		var style domain.HairStyle
		if err := rows.Scan(&style.Media.AssetID, &style.Media.MIMEType, &style.MediaObjectKey, &style.Media.SourceKind,
			&style.Media.DisplayLabel, &style.ID, &style.Name, &style.Reason); err != nil {
			return nil, err
		}
		out = append(out, style)
	}
	return out, rows.Err()
}
