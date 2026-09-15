package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/advisor"
	"github.com/zhanshimian/server/internal/service/diagnostic"
	"github.com/zhanshimian/server/internal/service/hair"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/share"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/service/wardrobe"
)

// 编译期契约：Store 实现全部七个外围 Reader。
var (
	_ home.Reader       = (*Store)(nil)
	_ today.Reader      = (*Store)(nil)
	_ wardrobe.Reader   = (*Store)(nil)
	_ advisor.Reader    = (*Store)(nil)
	_ hair.Reader       = (*Store)(nil)
	_ diagnostic.Reader = (*Store)(nil)
	_ share.Reader      = (*Store)(nil)
)

// ---- 首页聚合 ----

func (s *Store) ReadHome(ctx context.Context, userID string, now time.Time) (home.Snapshot, error) {
	snap := home.Snapshot{ActiveOperations: []domain.OperationRef{}}

	profile, err := s.readHomeProfile(ctx, userID)
	if err == nil {
		snap.Profile = profile
	} else if !isNotFound(err) {
		return snap, err
	}

	operations, err := s.readHomeActiveOperations(ctx, userID)
	if err != nil {
		return snap, err
	}
	snap.ActiveOperations = operations

	// 报告/方案集/今日/权益不是纯 SQL 投影：分别经 assessment/planning/
	// today/billing 的公开读路径由 home service 组合，Reader 不越权。
	return snap, nil
}

// LatestPublishedPlanSetID 定位用户最近一个已发布方案集（发布判定与
// planning 读模型同一 guard：关联 operation 已 succeeded）。
func (s *Store) LatestPublishedPlanSetID(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT ps.id::text FROM plan_sets ps
		WHERE ps.user_id=$1::uuid AND `+planningPublishedGuard+`
		ORDER BY ps.created_at DESC, ps.id DESC
		LIMIT 1`, userID).Scan(&id)
	if err != nil {
		return "", mapNotFound(err)
	}
	return id, nil
}

// MediaObjectInfo 按资产 ID 读对象定位；越权/不存在/已删除一律 ErrNotFound。
func (s *Store) MediaObjectInfo(ctx context.Context, userID, assetID string) (home.MediaObject, error) {
	var object home.MediaObject
	err := s.pool.QueryRow(ctx, `
		SELECT object_key, mime_type
		FROM media_assets
		WHERE user_id=$1::uuid AND id=$2::uuid AND state<>'deleted'`, userID, assetID).
		Scan(&object.ObjectKey, &object.MIMEType)
	if err != nil {
		return home.MediaObject{}, mapNotFound(err)
	}
	return object, nil
}

func (s *Store) readHomeProfile(ctx context.Context, userID string) (*domain.ProfileSummary, error) {
	var p domain.ProfileSummary
	var weight, bust, waist, hip *float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(height_cm,0), role, budget,
		       NULLIF(preferences->>'weight_kg','')::float8,
		       NULLIF(preferences->>'bust_cm','')::float8,
		       NULLIF(preferences->>'waist_cm','')::float8,
		       NULLIF(preferences->>'hip_cm','')::float8
		FROM user_profiles WHERE user_id=$1::uuid`, userID).
		Scan(&p.HeightCM, &p.Role, &p.Budget, &weight, &bust, &waist, &hip)
	if err != nil {
		return nil, mapNotFound(err)
	}
	p.WeightKG, p.BustCM, p.WaistCM, p.HipCM = weight, bust, waist, hip
	return &p, nil
}

func (s *Store) readHomeActiveOperations(ctx context.Context, userID string) ([]domain.OperationRef, error) {
	// failed 也下发：active_operations 的语义是「需要用户关注的操作」——
	// 我的页任务中心靠它呈现「未完成，点击查看」；首页轮询消费方按 IN_FLIGHT
	// 集合（accepted/running/retrying）自行过滤，不会被 failed 行误导。
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, kind, status
		FROM operations
		WHERE user_id=$1::uuid AND status IN ('accepted','running','retrying','failed')
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.OperationRef{}
	for rows.Next() {
		var ref domain.OperationRef
		if err := rows.Scan(&ref.ID, &ref.Kind, &ref.Status); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// ---- 今日 grounding ----

func (s *Store) ReadTodayGrounding(ctx context.Context, userID string) (today.Grounding, error) {
	var g today.Grounding

	var reportID *string
	var prefs, avoids []byte
	err := s.pool.QueryRow(ctx, `
		SELECT up.current_report_id::text, up.role, COALESCE(up.height_cm,0), up.budget,
		       up.preferences, up.avoidances
		FROM user_profiles up WHERE up.user_id=$1::uuid`, userID).
		Scan(&reportID, &g.Profile.Role, &g.Profile.HeightCM, &g.Profile.Budget, &prefs, &avoids)
	if err == nil {
		g.Profile.Preferences = json.RawMessage(prefs)
		g.Profile.Avoidances = json.RawMessage(avoids)
		if reportID != nil {
			g.ReportID = *reportID
		}
	} else if !isNotFound(err) {
		return g, err
	}

	if g.ReportID != "" {
		findings, err := s.readReportFindings(ctx, userID, g.ReportID)
		if err != nil {
			return g, err
		}
		g.Findings = findings
	}

	selected, err := s.readSelectedPlanGrounding(ctx, userID)
	if err == nil {
		g.SelectedPlan = selected
	}

	publication, err := s.readSelectedPublication(ctx, userID)
	if err == nil {
		g.Publication = publication
	}

	return g, nil
}

// ---- 衣橱 grounding ----

func (s *Store) ReadWardrobeGrounding(ctx context.Context, userID string) (wardrobe.Grounding, error) {
	var g wardrobe.Grounding

	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(current_report_id::text,'') FROM user_profiles WHERE user_id=$1::uuid`, userID).
		Scan(&g.CurrentReportID)
	if err != nil && !isNotFound(err) {
		return g, err
	}

	selected, err := s.readSelectedPlanGrounding(ctx, userID)
	if err == nil {
		g.SelectedPlan = selected
	}
	return g, nil
}

// ---- 顾问 grounding ----

func (s *Store) ReadAdvisorGrounding(ctx context.Context, userID string) (advisor.Grounding, error) {
	var g advisor.Grounding

	var reportID *string
	var prefs, avoids []byte
	err := s.pool.QueryRow(ctx, `
		SELECT up.current_report_id::text, up.role, COALESCE(up.height_cm,0), up.budget,
		       up.preferences, up.avoidances
		FROM user_profiles up WHERE up.user_id=$1::uuid`, userID).
		Scan(&reportID, &g.Profile.Role, &g.Profile.HeightCM, &g.Profile.Budget, &prefs, &avoids)
	if err == nil {
		g.Profile.Preferences = json.RawMessage(prefs)
		g.Profile.Avoidances = json.RawMessage(avoids)
	} else if !isNotFound(err) {
		return g, err
	}

	if reportID != nil {
		report, err := s.readReportGrounding(ctx, userID, *reportID)
		if err == nil {
			g.Report = report
		}
	}

	selected, err := s.readSelectedPlanGrounding(ctx, userID)
	if err == nil {
		g.SelectedPlan = selected
	}

	items, err := s.readWardrobeItems(ctx, userID, 50)
	if err != nil {
		return g, err
	}
	g.Wardrobe = items

	memories, err := s.readFeedbackMemories(ctx, userID)
	if err != nil {
		return g, err
	}
	g.Feedback = memories

	return g, nil
}

// ---- 发型 grounding ----

func (s *Store) ReadHairGrounding(ctx context.Context, userID string, reportID string) (hair.Grounding, error) {
	g := hair.Grounding{ReportID: reportID}

	findings, err := s.readReportFindings(ctx, userID, reportID)
	if err != nil {
		return g, err
	}
	g.Findings = findings

	var face domain.MediaInput
	err = s.pool.QueryRow(ctx, `
		SELECT ma.id::text, ma.object_key, ma.mime_type
		FROM reports r
		JOIN photo_set_items psi ON psi.user_id=r.user_id AND psi.photo_set_id=r.photo_set_id AND psi.role='face'
		JOIN media_assets ma ON ma.user_id=psi.user_id AND ma.id=psi.media_asset_id
		WHERE r.user_id=$1::uuid AND r.id=$2::uuid`, userID, reportID).
		Scan(&face.AssetID, &face.ObjectKey, &face.MIMEType)
	if err != nil {
		return g, mapNotFound(err)
	}
	g.Face = face
	return g, nil
}

// ---- 诊断 grounding ----

func (s *Store) ReadDiagnosticGrounding(ctx context.Context, userID string, reportID string, kind string) (diagnostic.Grounding, error) {
	var g diagnostic.Grounding

	var prefs, avoids []byte
	err := s.pool.QueryRow(ctx, `
		SELECT up.role, COALESCE(up.height_cm,0), up.budget, up.preferences, up.avoidances
		FROM user_profiles up WHERE up.user_id=$1::uuid`, userID).
		Scan(&g.Profile.Role, &g.Profile.HeightCM, &g.Profile.Budget, &prefs, &avoids)
	if err == nil {
		g.Profile.Preferences = json.RawMessage(prefs)
		g.Profile.Avoidances = json.RawMessage(avoids)
	} else if !isNotFound(err) {
		return g, err
	}

	if reportID == "" {
		// report_id 缺省回退用户最新报告（旧线 LatestReport 语义；reports 行只在
		// 分析完成后写入，即已发布）。用户还没有任何报告时照常诊断，只是缺少
		// 报告 grounding。
		latest, latestErr := s.latestReportID(ctx, userID)
		switch {
		case latestErr == nil:
			reportID = latest
		case !isNotFound(latestErr):
			return g, latestErr
		}
	}
	if reportID != "" {
		report, err := s.readReportGrounding(ctx, userID, reportID)
		if err == nil {
			g.Report = report
		}
	}

	// 只有购买判断需要衣橱上下文；穿搭诊断不读衣橱（outfit 省略）。
	if kind == "purchase" {
		items, err := s.readWardrobeItems(ctx, userID, 20)
		if err != nil {
			return g, err
		}
		g.Wardrobe = items
	}

	return g, nil
}

// latestReportID 定位用户最新一份报告（created_at 与 id 双键定序，与
// LatestPublishedPlanSetID 同一定序习惯）。
func (s *Store) latestReportID(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text FROM reports WHERE user_id=$1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, userID).Scan(&id)
	if err != nil {
		return "", mapNotFound(err)
	}
	return id, nil
}

// ReadDiagnosticMedia 读诊断源照片的对象定位；越权/不存在/已删除一律
// ErrNotFound（与 MediaObjectInfo 同一 guard）。
func (s *Store) ReadDiagnosticMedia(ctx context.Context, userID string, assetID string) (domain.MediaInput, error) {
	var media domain.MediaInput
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, object_key, mime_type
		FROM media_assets
		WHERE user_id=$1::uuid AND id=$2::uuid AND state<>'deleted'`, userID, assetID).
		Scan(&media.AssetID, &media.ObjectKey, &media.MIMEType)
	if err != nil {
		return domain.MediaInput{}, mapNotFound(err)
	}
	return media, nil
}

// ---- 分享来源 ----

func (s *Store) ReadShareSource(ctx context.Context, userID string, sourceType string, sourceID string) (share.Source, error) {
	switch sourceType {
	case "plan_variant":
		return s.readSharePlanVariant(ctx, userID, sourceID)
	case "today_plan":
		return s.readShareTodayPlan(ctx, userID, sourceID)
	default:
		return share.Source{}, repository.ErrNotFound
	}
}

func (s *Store) readSharePlanVariant(ctx context.Context, userID string, sourceID string) (share.Source, error) {
	var source share.Source
	err := s.pool.QueryRow(ctx, `
		SELECT 'plan_variant', pv.id::text, pv.name, pv.descriptor,
		       ma.id::text, ma.object_key, ma.mime_type,
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
		       rp.created_at
		FROM plan_variants pv
		JOIN render_heads rh ON rh.user_id = pv.user_id AND rh.plan_variant_id = pv.id
		JOIN render_publications rp ON rp.user_id = rh.user_id AND rp.id = rh.current_publication_id
		JOIN render_candidates rc ON rc.user_id = rp.user_id AND rc.id = rp.candidate_id
		JOIN media_assets ma ON ma.user_id = rc.user_id AND ma.id = rc.asset_id
		WHERE pv.user_id = $1 AND pv.id = $2::uuid
		  AND ma.state = 'published'`,
		userID, sourceID).
		Scan(&source.SourceType, &source.SourceID, &source.Title, &source.Summary,
			&source.AssetID, &source.ObjectKey, &source.MIMEType, &source.SourceKind, &source.DisplayLabel,
			&source.PublishedAt)
	return source, mapNotFound(err)
}

// readShareTodayPlan：今日方案目前没有任何渲染写路径回填 render_publication_id
// （恒 NULL，见 today.Generate——只生成文本，不触发渲染）。因此按 LEFT JOIN 读：
// 有发布媒体时带资产身份，没有时退化为纯文本快照卡（title/summary），而不是 404。
// 一旦渲染链路接回并回填该列，同一查询自动带上媒体。
func (s *Store) readShareTodayPlan(ctx context.Context, userID string, sourceID string) (share.Source, error) {
	var source share.Source
	var assetID, objectKey, mimeType, sourceKind, displayLabel *string
	var publishedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT 'today_plan', tp.id::text, tp.title, tp.summary,
		       ma.id::text, ma.object_key, ma.mime_type,
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
		       rp.created_at
		FROM today_plans tp
		LEFT JOIN render_publications rp ON rp.user_id = tp.user_id AND rp.id = tp.render_publication_id
		LEFT JOIN render_candidates rc ON rc.user_id = rp.user_id AND rc.id = rp.candidate_id
		LEFT JOIN media_assets ma ON ma.user_id = rc.user_id AND ma.id = rc.asset_id
		  AND ma.state = 'published'
		WHERE tp.user_id = $1 AND tp.id = $2::uuid`,
		userID, sourceID).
		Scan(&source.SourceType, &source.SourceID, &source.Title, &source.Summary,
			&assetID, &objectKey, &mimeType, &sourceKind, &displayLabel, &publishedAt)
	if err != nil {
		return source, mapNotFound(err)
	}
	source.AssetID = deref(assetID)
	source.ObjectKey = deref(objectKey)
	source.MIMEType = deref(mimeType)
	if sourceKind != nil {
		source.SourceKind = domain.MediaSourceKind(*sourceKind)
	}
	source.DisplayLabel = deref(displayLabel)
	if publishedAt != nil {
		source.PublishedAt = *publishedAt
	}
	return source, nil
}

// ---- 共享子查询 ----

func (s *Store) readReportFindings(ctx context.Context, userID string, reportID string) ([]domain.FindingGrounding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT rf.id::text, rf.category, rf.label, rf.visible_observation, rf.recommendation,
		       rf.priority, psi.role
		FROM report_findings rf
		JOIN photo_set_items psi ON psi.user_id=rf.user_id AND psi.id=rf.source_photo_item_id
		WHERE rf.user_id=$1::uuid AND rf.report_id=$2::uuid
		ORDER BY rf.position`, userID, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FindingGrounding{}
	for rows.Next() {
		var f domain.FindingGrounding
		if err := rows.Scan(&f.ID, &f.Category, &f.Label, &f.VisibleObservation, &f.Recommendation,
			&f.Priority, &f.SourceRole); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) readReportGrounding(ctx context.Context, userID string, reportID string) (*domain.ReportGrounding, error) {
	var report domain.ReportGrounding
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, priority_title, priority_copy, impression_tags
		FROM reports WHERE user_id=$1::uuid AND id=$2::uuid`, userID, reportID).
		Scan(&report.ID, &report.PriorityTitle, &report.PriorityCopy, &report.ImpressionTags)
	if err != nil {
		return nil, mapNotFound(err)
	}
	findings, err := s.readReportFindings(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	report.Findings = findings
	return &report, nil
}

func (s *Store) readSelectedPlanGrounding(ctx context.Context, userID string) (*domain.PlanVariantGrounding, error) {
	var g domain.PlanVariantGrounding
	err := s.pool.QueryRow(ctx, `
		SELECT pv.id::text, pv.plan_set_id::text, pv.name, pv.descriptor, pv.outcome_tags
		FROM plan_selections ps
		JOIN plan_variants pv ON pv.user_id=ps.user_id AND pv.id=ps.plan_variant_id
		WHERE ps.user_id=$1::uuid
		ORDER BY ps.created_at DESC LIMIT 1`, userID).
		Scan(&g.ID, &g.PlanSetID, &g.Name, &g.Descriptor, &g.OutcomeTags)
	if err != nil {
		return nil, mapNotFound(err)
	}
	steps, err := s.readPlanVariantSteps(ctx, userID, g.ID)
	if err != nil {
		return nil, err
	}
	g.Steps = steps
	return &g, nil
}

func (s *Store) readPlanVariantSteps(ctx context.Context, userID string, variantID string) ([]domain.PlanStep, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, plan_variant_id::text, category, action, title, summary,
		       details, position, created_at
		FROM plan_steps
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid
		ORDER BY position`, userID, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PlanStep{}
	for rows.Next() {
		var step domain.PlanStep
		var details []byte
		if err := rows.Scan(&step.ID, &step.UserID, &step.PlanVariantID, &step.Category, &step.Action,
			&step.Title, &step.Summary, &details, &step.Position, &step.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(details, &step.Details); err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func (s *Store) readSelectedPublication(ctx context.Context, userID string) (*domain.PublishedMedia, error) {
	var media domain.PublishedMedia
	err := s.pool.QueryRow(ctx, `
		SELECT ma.id::text, ma.object_key,
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
		       rp.created_at
		FROM plan_selections ps
		JOIN render_heads rh ON rh.user_id=ps.user_id AND rh.plan_variant_id=ps.plan_variant_id
		JOIN render_publications rp ON rp.user_id=rh.user_id AND rp.id=rh.current_publication_id
		JOIN render_candidates rc ON rc.user_id=rp.user_id AND rc.id=rp.candidate_id
		JOIN media_assets ma ON ma.user_id=rc.user_id AND ma.id=rc.asset_id
		WHERE ps.user_id=$1::uuid AND ma.state='published'
		ORDER BY ps.created_at DESC LIMIT 1`, userID).
		Scan(&media.AssetID, &media.ObjectKey, &media.SourceKind, &media.DisplayLabel, &media.PublishedAt)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return &media, nil
}

func (s *Store) readWardrobeItems(ctx context.Context, userID string, limit int) ([]domain.WardrobeGroundingItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT wi.id::text, wi.name, wi.category, wi.color, wi.formality,
		       COALESCE(ma.object_key,'')
		FROM wardrobe_items wi
		LEFT JOIN media_assets ma ON ma.user_id=wi.user_id AND ma.id=wi.media_asset_id
		WHERE wi.user_id=$1::uuid
		ORDER BY wi.created_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WardrobeGroundingItem{}
	for rows.Next() {
		var item domain.WardrobeGroundingItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Color, &item.Formality, &item.ObjectKey); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) readFeedbackMemories(ctx context.Context, userID string) ([]domain.FeedbackMemoryItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, memory_key, category, value
		FROM preference_memories
		WHERE user_id=$1::uuid
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FeedbackMemoryItem{}
	for rows.Next() {
		var item domain.FeedbackMemoryItem
		if err := rows.Scan(&item.ID, &item.Key, &item.Category, &item.Value); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func isNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, repository.ErrNotFound)
}
