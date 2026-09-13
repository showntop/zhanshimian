package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/today"
)

var _ today.Writer = (*Store)(nil)

func (s *Store) CreateTodayPlan(ctx context.Context, userID string, plan today.Plan) (today.Plan, error) {
	steps, err := json.Marshal(plan.Steps)
	if err != nil {
		return plan, err
	}
	contextJSON, err := json.Marshal(plan.Context)
	if err != nil {
		return plan, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO today_plans(user_id, report_id, context, title, summary, steps, active, state, feedback)
		VALUES ($1::uuid, NULLIF($2::uuid,''), $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, created_at, updated_at`,
		userID, plan.ReportID, contextJSON, plan.Title, plan.Summary, steps,
		plan.Active, plan.State, plan.Feedback).
		Scan(&plan.ID, &plan.CreatedAt, &plan.UpdatedAt)
	return plan, mapNotFound(err)
}

func (s *Store) CurrentTodayPlan(ctx context.Context, userID string) (today.Plan, error) {
	return s.scanTodayPlan(s.pool.QueryRow(ctx, todayPlanSelectSQL+`
		WHERE tp.user_id=$1::uuid AND tp.active
		ORDER BY tp.updated_at DESC LIMIT 1`, userID))
}

func (s *Store) MarkTodayPlanActive(ctx context.Context, userID string, id string) (today.Plan, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return today.Plan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE today_plans SET active=false WHERE user_id=$1::uuid`, userID); err != nil {
		return today.Plan{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE today_plans SET active=true, updated_at=now() WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id); err != nil {
		return today.Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return today.Plan{}, err
	}
	return s.CurrentTodayPlan(ctx, userID)
}

func (s *Store) RecordTodayPlanFeedback(ctx context.Context, userID string, id string, feedback string) (today.Plan, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE today_plans SET feedback=$3, updated_at=now() WHERE user_id=$1::uuid AND id=$2::uuid`,
		userID, id, feedback)
	if err != nil {
		return today.Plan{}, err
	}
	if tag.RowsAffected() == 0 {
		return today.Plan{}, repository.ErrNotFound
	}
	return s.scanTodayPlan(s.pool.QueryRow(ctx, todayPlanSelectSQL+` WHERE tp.user_id=$1::uuid AND tp.id=$2::uuid`, userID, id))
}

const todayPlanSelectSQL = `
	SELECT tp.id::text, COALESCE(tp.report_id::text,''), tp.context, tp.title, tp.summary,
	       tp.steps, tp.active, tp.state, tp.feedback,
	       COALESCE(op.id::text,''), COALESCE(op.kind::text,''), COALESCE(op.status::text,''),
	       COALESCE(ma.id::text,''), COALESCE(ma.object_key,''),
	       CASE ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END,
	       tp.created_at, tp.updated_at
	FROM today_plans tp
	LEFT JOIN operations op ON op.user_id=tp.user_id AND op.id=tp.operation_id
	LEFT JOIN render_publications rp ON rp.user_id=tp.user_id AND rp.id=tp.render_publication_id
	LEFT JOIN render_candidates rc ON rc.user_id=rp.user_id AND rc.id=rp.candidate_id
	LEFT JOIN media_assets ma ON ma.user_id=rc.user_id AND ma.id=rc.asset_id`

func (s *Store) scanTodayPlan(row rowScanner) (today.Plan, error) {
	var plan today.Plan
	var contextJSON, stepsJSON []byte
	var feedback *string
	var mediaAssetID, mediaObjectKey, mediaSourceKind, mediaDisplayLabel string
	err := row.Scan(
		&plan.ID, &plan.ReportID, &contextJSON, &plan.Title, &plan.Summary,
		&stepsJSON, &plan.Active, &plan.State, &feedback,
		&plan.Operation.ID, &plan.Operation.Kind, &plan.Operation.Status,
		&mediaAssetID, &mediaObjectKey, &mediaSourceKind, &mediaDisplayLabel,
		&plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		return plan, err
	}
	if err := json.Unmarshal(contextJSON, &plan.Context); err != nil {
		return plan, err
	}
	if err := json.Unmarshal(stepsJSON, &plan.Steps); err != nil {
		return plan, err
	}
	if feedback != nil {
		plan.Feedback = feedback
	}
	if mediaAssetID != "" {
		plan.Media = &domain.RenderMediaView{
			AssetID: mediaAssetID, SourceKind: mediaSourceKind, DisplayLabel: mediaDisplayLabel,
		}
	}
	return plan, nil
}
