package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

func (s *Store) CreateSession(ctx context.Context, openID, nickname string, digest []byte, expiresAt time.Time) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	var user domain.User
	err = tx.QueryRow(ctx, `
		INSERT INTO users(open_id,nickname) VALUES($1,$2)
		ON CONFLICT(open_id) DO UPDATE SET nickname=EXCLUDED.nickname, updated_at=now()
		RETURNING id::text,nickname`, openID, nickname).Scan(&user.ID, &user.Nickname)
	if err != nil {
		return domain.User{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_sessions(user_id,token_digest,expires_at) VALUES($1,$2,$3)`, user.ID, digest, expiresAt)
	if err != nil {
		return domain.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) UserByTokenDigest(ctx context.Context, digest []byte) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text,u.nickname FROM user_sessions ss
		JOIN users u ON u.id=ss.user_id
		WHERE ss.token_digest=$1 AND ss.expires_at>now()`, digest).Scan(&user.ID, &user.Nickname)
	return user, mapNotFound(err)
}

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

func (s *Store) CreateAnalysis(ctx context.Context, userID string, input domain.CreateAnalysisInput) (domain.Analysis, error) {
	if len(input.MediaIDs) != 3 {
		return domain.Analysis{}, fmt.Errorf("exactly three media assets are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Analysis{}, err
	}
	defer tx.Rollback(ctx)
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM media_assets WHERE user_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL`, userID, input.MediaIDs).Scan(&count)
	if err != nil {
		return domain.Analysis{}, err
	}
	if count != 3 {
		return domain.Analysis{}, fmt.Errorf("one or more media assets are unavailable")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return domain.Analysis{}, err
	}
	profile, err := json.Marshal(input.Profile)
	if err != nil {
		return domain.Analysis{}, err
	}
	var analysis domain.Analysis
	err = tx.QueryRow(ctx, `
		INSERT INTO analyses(user_id,scene,media_ids,profile,status,progress,stage)
		VALUES($1,$2,$3,$4,'queued',5,'正在安全上传照片')
		RETURNING id::text,status,progress,stage,created_at,updated_at`, userID, input.Scene, input.MediaIDs, profile).
		Scan(&analysis.ID, &analysis.Status, &analysis.Progress, &analysis.Stage, &analysis.CreatedAt, &analysis.UpdatedAt)
	if err != nil {
		return domain.Analysis{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO analysis_jobs(analysis_id,user_id,payload) VALUES($1,$2,$3)`, analysis.ID, userID, payload)
	if err != nil {
		return domain.Analysis{}, err
	}
	return analysis, tx.Commit(ctx)
}

func (s *Store) GetAnalysis(ctx context.Context, userID, analysisID string) (domain.Analysis, error) {
	var item domain.Analysis
	err := s.pool.QueryRow(ctx, `
		SELECT a.id::text,a.status,a.progress,a.stage,a.error_message,coalesce(r.id::text,''),a.created_at,a.updated_at
		FROM analyses a LEFT JOIN reports r ON r.analysis_id=a.id
		WHERE a.id=$1 AND a.user_id=$2`, analysisID, userID).
		Scan(&item.ID, &item.Status, &item.Progress, &item.Stage, &item.ErrorMessage, &item.ReportID, &item.CreatedAt, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) ClaimAnalysisJob(ctx context.Context) (domain.AnalysisJob, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AnalysisJob{}, false, err
	}
	defer tx.Rollback(ctx)
	var job domain.AnalysisJob
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT id::text,analysis_id::text,user_id::text,attempts,payload
		FROM analysis_jobs
		WHERE status='queued' AND next_run_at<=now()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&job.ID, &job.AnalysisID, &job.UserID, &job.Attempt, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnalysisJob{}, false, nil
	}
	if err != nil {
		return domain.AnalysisJob{}, false, err
	}
	if err := json.Unmarshal(payload, &job.Input); err != nil {
		return domain.AnalysisJob{}, false, err
	}
	job.Attempt++
	_, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status='running',attempts=$2,locked_at=now(),updated_at=now() WHERE id=$1`, job.ID, job.Attempt)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE analyses SET status='processing',progress=24,stage='读取面部与头肩比例',updated_at=now() WHERE id=$1`, job.AnalysisID)
	}
	if err != nil {
		return domain.AnalysisJob{}, false, err
	}
	return job, true, tx.Commit(ctx)
}

func (s *Store) UpdateAnalysisProgress(ctx context.Context, analysisID string, progress int, stage string) error {
	_, err := s.pool.Exec(ctx, `UPDATE analyses SET progress=$2,stage=$3,updated_at=now() WHERE id=$1`, analysisID, progress, stage)
	return err
}

func (s *Store) CompleteAnalysis(ctx context.Context, job domain.AnalysisJob, output domain.AnalysisOutput) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var reportID string
	err = tx.QueryRow(ctx, `
		INSERT INTO reports(analysis_id,user_id,current_image_url,impression_tags,priority_title,priority_copy,provider_version)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, job.AnalysisID, job.UserID, output.CurrentImageURL, output.ImpressionTags, output.PriorityTitle, output.PriorityCopy, output.ProviderVersion).Scan(&reportID)
	if err != nil {
		return "", err
	}
	for index, finding := range output.Findings {
		_, err = tx.Exec(ctx, `INSERT INTO report_findings(report_id,label,category,severity,anchor_x,anchor_y,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7)`, reportID, finding.Label, finding.Category, finding.Severity, finding.AnchorX, finding.AnchorY, index+1)
		if err != nil {
			return "", err
		}
	}
	for _, plan := range output.Plans {
		var planID string
		err = tx.QueryRow(ctx, `
			INSERT INTO plans(report_id,user_id,name,slug,image_url,recommended,descriptor,why,outcome_tags,difference_tags,sort_order)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`, reportID, job.UserID, plan.Name, plan.Slug, plan.ImageURL, plan.Recommended, plan.Descriptor, plan.Why, plan.OutcomeTags, plan.DifferenceTags, plan.Sort).Scan(&planID)
		if err != nil {
			return "", err
		}
		for _, step := range plan.Steps {
			_, err = tx.Exec(ctx, `INSERT INTO plan_steps(plan_id,category,title,summary,details,sort_order) VALUES($1,$2,$3,$4,$5,$6)`, planID, step.Category, step.Title, step.Summary, step.Details, step.Sort)
			if err != nil {
				return "", err
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE analyses SET status='completed',progress=100,stage='三套方案已经准备好',updated_at=now() WHERE id=$1`, job.AnalysisID)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status='done',updated_at=now() WHERE id=$1`, job.ID)
	}
	if err != nil {
		return "", err
	}
	return reportID, tx.Commit(ctx)
}

func (s *Store) FailAnalysis(ctx context.Context, job domain.AnalysisJob, cause error) error {
	message := "分析暂时没有完成，请稍后重试"
	if job.Attempt >= 3 {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `UPDATE analysis_jobs SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, job.ID, cause.Error()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE analyses SET status='failed',error_message=$2,stage='分析未完成',updated_at=now() WHERE id=$1`, job.AnalysisID, message); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	delay := time.Duration(job.Attempt*job.Attempt) * time.Second
	_, err := s.pool.Exec(ctx, `UPDATE analysis_jobs SET status='queued',last_error=$2,next_run_at=$3,updated_at=now() WHERE id=$1`, job.ID, cause.Error(), time.Now().Add(delay))
	return err
}

func (s *Store) GetReport(ctx context.Context, userID, reportID string) (domain.Report, error) {
	var report domain.Report
	err := s.pool.QueryRow(ctx, `SELECT id::text,analysis_id::text,current_image_url,impression_tags,priority_title,priority_copy,provider_version,generated_at FROM reports WHERE id=$1 AND user_id=$2`, reportID, userID).
		Scan(&report.ID, &report.AnalysisID, &report.CurrentImageURL, &report.ImpressionTags, &report.PriorityTitle, &report.PriorityCopy, &report.ProviderVersion, &report.GeneratedAt)
	if err != nil {
		return report, mapNotFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,label,category,severity,anchor_x,anchor_y FROM report_findings WHERE report_id=$1 ORDER BY sort_order`, reportID)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var finding domain.Finding
		if err := rows.Scan(&finding.ID, &finding.Label, &finding.Category, &finding.Severity, &finding.AnchorX, &finding.AnchorY); err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, finding)
	}
	return report, rows.Err()
}

func scanPlan(row pgx.Row) (domain.Plan, error) {
	var item domain.Plan
	err := row.Scan(&item.ID, &item.ReportID, &item.Name, &item.Slug, &item.ImageURL, &item.Recommended, &item.Descriptor, &item.Why, &item.OutcomeTags, &item.DifferenceTags, &item.Sort, &item.Selected)
	return item, err
}

const planSelect = `SELECT id::text,report_id::text,name,slug,image_url,recommended,descriptor,why,outcome_tags,difference_tags,sort_order,(selected_at IS NOT NULL) FROM plans`

func (s *Store) ListPlans(ctx context.Context, userID, reportID string) ([]domain.Plan, error) {
	rows, err := s.pool.Query(ctx, planSelect+` WHERE report_id=$1 AND user_id=$2 ORDER BY sort_order`, reportID, userID)
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

func (s *Store) GetPlan(ctx context.Context, userID, planID string) (domain.Plan, error) {
	item, err := scanPlan(s.pool.QueryRow(ctx, planSelect+` WHERE id=$1 AND user_id=$2`, planID, userID))
	if err != nil {
		return item, mapNotFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,category,title,summary,details,sort_order FROM plan_steps WHERE plan_id=$1 ORDER BY sort_order`, planID)
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

func (s *Store) SelectPlan(ctx context.Context, userID, planID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reportID string
	if err = tx.QueryRow(ctx, `SELECT report_id::text FROM plans WHERE id=$1 AND user_id=$2 FOR UPDATE`, planID, userID).Scan(&reportID); err != nil {
		return mapNotFound(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE plans SET selected_at=NULL WHERE report_id=$1`, reportID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE plans SET selected_at=now() WHERE id=$1`, planID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO checklist_items(plan_id,user_id,category,title,description,meta,sort_order)
		SELECT ps.plan_id,$2,ps.category,ps.title,ps.summary,
		CASE ps.category WHEN 'hair' THEN '给发型师看参考卡' WHEN 'makeup' THEN '预计 8 分钟' ELSE '优先使用现有衣橱' END,
		ps.sort_order FROM plan_steps ps WHERE ps.plan_id=$1
		ON CONFLICT(plan_id,category) DO NOTHING`, planID, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetChecklist(ctx context.Context, userID, planID string) ([]domain.ChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,plan_id::text,category,title,description,meta,completed,sort_order FROM checklist_items WHERE plan_id=$1 AND user_id=$2 ORDER BY sort_order`, planID, userID)
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

func (s *Store) SetChecklistItem(ctx context.Context, userID, itemID string, completed bool) (domain.ChecklistItem, error) {
	var item domain.ChecklistItem
	err := s.pool.QueryRow(ctx, `
		UPDATE checklist_items SET completed=$3,updated_at=now() WHERE id=$1 AND user_id=$2
		RETURNING id::text,plan_id::text,category,title,description,meta,completed,sort_order`, itemID, userID, completed).
		Scan(&item.ID, &item.PlanID, &item.Category, &item.Title, &item.Description, &item.Meta, &item.Completed, &item.Sort)
	return item, mapNotFound(err)
}

func (s *Store) AddFeedback(ctx context.Context, userID string, input domain.FeedbackInput) error {
	if _, err := uuid.Parse(input.PlanID); err != nil {
		return repository.ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO feedback(user_id,plan_id,tags,comment) SELECT $1,p.id,$3,$4 FROM plans p WHERE p.id=$2 AND p.user_id=$1`, userID, input.PlanID, input.Tags, input.Comment)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (s *Store) CreateToolResult(ctx context.Context, userID string, input domain.ToolInput, result domain.ToolResult) (domain.ToolResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ToolResult{}, err
	}
	defer tx.Rollback(ctx)
	var reportID any
	if input.ReportID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=$1 AND user_id=$2)`, input.ReportID, userID).Scan(&exists); err != nil {
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
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`, input.MediaID, userID).Scan(&exists); err != nil {
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
	return result, tx.Commit(ctx)
}

func (s *Store) SaveToolResult(ctx context.Context, userID, resultID string) (domain.ToolResult, error) {
	var result domain.ToolResult
	var payload []byte
	var kind, scene string
	var createdAt time.Time
	var saved bool
	err := s.pool.QueryRow(ctx, `
		UPDATE tool_results SET saved=true,updated_at=now()
		WHERE id=$1 AND user_id=$2
		RETURNING id::text,kind,scene,payload,saved,created_at`, resultID, userID).
		Scan(&result.ID, &kind, &scene, &payload, &saved, &createdAt)
	if err != nil {
		return domain.ToolResult{}, mapNotFound(err)
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return domain.ToolResult{}, err
	}
	result.ID = resultID
	result.Kind = kind
	result.Scene = scene
	result.Saved = saved
	result.CreatedAt = createdAt
	return result, nil
}

func (s *Store) CreateHairPreview(ctx context.Context, userID string, input domain.HairPreviewInput, styleName string) (domain.HairPreview, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.HairPreview{}, err
	}
	defer tx.Rollback(ctx)
	var storageKey string
	if err := tx.QueryRow(ctx, `SELECT storage_key FROM media_assets WHERE id=$1 AND user_id=$2 AND kind='face' AND deleted_at IS NULL`, input.MediaID, userID).Scan(&storageKey); err != nil {
		return domain.HairPreview{}, mapNotFound(err)
	}
	var reportID any
	if input.ReportID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=$1 AND user_id=$2)`, input.ReportID, userID).Scan(&exists); err != nil {
			return domain.HairPreview{}, err
		}
		if !exists {
			return domain.HairPreview{}, repository.ErrNotFound
		}
		reportID = input.ReportID
	}
	sourceURL := "/uploads/" + storageKey
	if strings.HasPrefix(storageKey, "demo/") {
		sourceURL = "/assets/looks/natural.png"
	}
	var preview domain.HairPreview
	err = tx.QueryRow(ctx, `
		INSERT INTO hair_previews(user_id,report_id,media_id,scene,style_id,style_name,source_image_url)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id::text,status,progress,stage,style_id,style_name,scene,source_image_url,result_image_url,provider_version,saved,error_message,created_at,updated_at`,
		userID, reportID, input.MediaID, input.Scene, input.StyleID, styleName, sourceURL).
		Scan(&preview.ID, &preview.Status, &preview.Progress, &preview.Stage, &preview.StyleID, &preview.StyleName, &preview.Scene, &preview.SourceImageURL, &preview.ResultImageURL, &preview.ProviderVersion, &preview.Saved, &preview.ErrorMessage, &preview.CreatedAt, &preview.UpdatedAt)
	if err != nil {
		return domain.HairPreview{}, err
	}
	return preview, tx.Commit(ctx)
}

const hairPreviewSelect = `SELECT id::text,status,progress,stage,style_id,style_name,scene,source_image_url,result_image_url,provider_version,saved,error_message,created_at,updated_at FROM hair_previews`

func scanHairPreview(row pgx.Row) (domain.HairPreview, error) {
	var item domain.HairPreview
	err := row.Scan(&item.ID, &item.Status, &item.Progress, &item.Stage, &item.StyleID, &item.StyleName, &item.Scene, &item.SourceImageURL, &item.ResultImageURL, &item.ProviderVersion, &item.Saved, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) GetHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	item, err := scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelect+` WHERE id=$1 AND user_id=$2`, previewID, userID))
	return item, mapNotFound(err)
}

func (s *Store) ListSavedHairPreviews(ctx context.Context, userID string) ([]domain.HairPreview, error) {
	rows, err := s.pool.Query(ctx, hairPreviewSelect+` WHERE user_id=$1 AND saved=true AND status='completed' ORDER BY updated_at DESC LIMIT 20`, userID)
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

func (s *Store) ClaimHairPreview(ctx context.Context) (domain.HairPreviewJob, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.HairPreviewJob{}, false, err
	}
	defer tx.Rollback(ctx)
	var job domain.HairPreviewJob
	err = tx.QueryRow(ctx, `
		SELECT id::text,user_id::text,attempts,media_id::text,coalesce(report_id::text,''),style_id,scene
		FROM hair_previews WHERE status='queued' AND next_run_at<=now()
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).
		Scan(&job.PreviewID, &job.UserID, &job.Attempt, &job.Input.MediaID, &job.Input.ReportID, &job.Input.StyleID, &job.Input.Scene)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HairPreviewJob{}, false, nil
	}
	if err != nil {
		return domain.HairPreviewJob{}, false, err
	}
	job.Attempt++
	_, err = tx.Exec(ctx, `UPDATE hair_previews SET status='processing',progress=32,stage='保留五官并重塑发型',attempts=$2,locked_at=now(),updated_at=now() WHERE id=$1`, job.PreviewID, job.Attempt)
	if err != nil {
		return domain.HairPreviewJob{}, false, err
	}
	return job, true, tx.Commit(ctx)
}

func (s *Store) CompleteHairPreview(ctx context.Context, job domain.HairPreviewJob, resultURL, storageKey, providerVersion string) error {
	_, err := s.pool.Exec(ctx, `UPDATE hair_previews SET status='completed',progress=100,stage='预览已生成',result_image_url=$2,result_storage_key=$3,provider_version=$4,error_message='',updated_at=now() WHERE id=$1`, job.PreviewID, resultURL, storageKey, providerVersion)
	return err
}

func (s *Store) FailHairPreview(ctx context.Context, job domain.HairPreviewJob, cause error) error {
	if job.Attempt >= 2 {
		_, err := s.pool.Exec(ctx, `UPDATE hair_previews SET status='failed',stage='生成未完成',error_message='预览暂时没有生成，请稍后重试',last_error=$2,updated_at=now() WHERE id=$1`, job.PreviewID, cause.Error())
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE hair_previews SET status='queued',progress=12,stage='正在重新尝试',last_error=$2,next_run_at=$3,updated_at=now() WHERE id=$1`, job.PreviewID, cause.Error(), time.Now().Add(time.Duration(job.Attempt)*time.Second))
	return err
}

func (s *Store) SaveHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	item, err := scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelect+` WHERE id=$1 AND user_id=$2 AND status='completed'`, previewID, userID))
	if err != nil {
		return domain.HairPreview{}, mapNotFound(err)
	}
	item.Saved = true
	_, err = s.pool.Exec(ctx, `UPDATE hair_previews SET saved=true,updated_at=now() WHERE id=$1 AND user_id=$2`, previewID, userID)
	return item, err
}

func (s *Store) DeleteUserData(ctx context.Context, userID string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT storage_key FROM media_assets WHERE user_id=$1 AND deleted_at IS NULL UNION ALL SELECT result_storage_key FROM hair_previews WHERE user_id=$1 AND result_storage_key<>''`, userID)
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
	if _, err = tx.Exec(ctx, `DELETE FROM tool_results WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM analyses WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM media_assets WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	return keys, tx.Commit(ctx)
}
