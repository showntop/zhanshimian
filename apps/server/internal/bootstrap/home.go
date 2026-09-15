package bootstrap

import (
	"context"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/storage"
)

// 本文件是首页聚合的装配适配器：home 只定义端口（不 import 兄弟 service
// 包，避免经 postgres 读模型形成测试期 import cycle），这里把具体服务的
// 公开读方法机械转换为 home 的端口视图类型。planning.Service 与
// billing.Orders 的方法签名与端口完全一致，在 api.go 直接注入。

// homeReportReader 复用 assessment 的报告读路径（含 source_media 签名
// URL），首页不维护第二份报告投影。
type homeReportReader struct {
	inner *assessment.Service
}

func (a homeReportReader) GetCurrentReport(ctx context.Context, userID string) (home.ReportView, error) {
	view, err := a.inner.GetCurrentReport(ctx, userID)
	if err != nil {
		return home.ReportView{}, err
	}
	findings := make([]home.FindingView, 0, len(view.Findings))
	for _, finding := range view.Findings {
		findings = append(findings, home.FindingView{
			ID:                 finding.ID,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			Priority:           finding.Priority,
			Position:           finding.Position,
			SourcePhoto: home.SourcePhotoRef{
				ItemID: finding.SourcePhoto.ItemID,
				Role:   finding.SourcePhoto.Role,
			},
			Anchor: finding.Anchor,
		})
	}
	return home.ReportView{
		ID:             view.ID,
		PhotoSetID:     view.PhotoSetID,
		HeroAssetID:    view.HeroAssetID,
		PriorityTitle:  view.PriorityTitle,
		PriorityCopy:   view.PriorityCopy,
		SchemaVersion:  view.SchemaVersion,
		ImpressionTags: view.ImpressionTags,
		SourceMedia: home.ReportSourceMedia{
			Face: homeReportPhoto(view.SourceMedia.Face),
			Side: homeReportPhoto(view.SourceMedia.Side),
			Body: homeReportPhoto(view.SourceMedia.Body),
		},
		Findings:  findings,
		CreatedAt: view.CreatedAt,
	}, nil
}

func homeReportPhoto(photo assessment.ReportPhoto) home.ReportPhoto {
	return home.ReportPhoto{
		ItemID: photo.ItemID,
		Role:   photo.Role,
		Media: home.PresentedMedia{
			AssetID:      photo.Media.AssetID,
			URL:          photo.Media.URL,
			URLExpiresAt: photo.Media.URLExpiresAt,
			MIMEType:     photo.Media.MIMEType,
			SourceKind:   photo.Media.SourceKind,
			DisplayLabel: photo.Media.DisplayLabel,
		},
	}
}

// homeTodayReader 复用 today 的当前方案读路径。
type homeTodayReader struct {
	inner *today.Service
}

func (a homeTodayReader) Current(ctx context.Context, userID string) (home.TodayPlan, error) {
	plan, err := a.inner.Current(ctx, userID)
	if err != nil {
		return home.TodayPlan{}, err
	}
	return home.TodayPlan{
		ID:        plan.ID,
		Context:   plan.Context,
		Title:     plan.Title,
		Summary:   plan.Summary,
		Steps:     plan.Steps,
		Active:    plan.Active,
		State:     plan.State,
		Operation: plan.Operation,
		Media:     plan.Media,
		Feedback:  plan.Feedback,
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}, nil
}

// homeMediaSigner 给 today_plan 的发布媒体解析可读 URL：COS 走短时签名；
// 本地存储没有签名能力时回退 PUBLIC_BASE_URL + /uploads/ 公开路径
// （与 bodyURLSigner/mediaPresenter 同一做法）。url_expires_at 语义是
// 「此刻起 ttl 内新鲜」，公开 URL 实际不过期。
type homeMediaSigner struct {
	signer        storage.SignedURLStorage
	ttl           time.Duration
	publicBaseURL string
}

func newHomeMediaSigner(objects storage.ObjectStorage, publicBaseURL string, ttl time.Duration) homeMediaSigner {
	var signer storage.SignedURLStorage
	if s, ok := objects.(storage.SignedURLStorage); ok {
		signer = s
	}
	return homeMediaSigner{signer: signer, ttl: ttl, publicBaseURL: strings.TrimRight(publicBaseURL, "/")}
}

func (s homeMediaSigner) SignedURL(ctx context.Context, objectKey string) (string, time.Time, error) {
	expiresAt := time.Now().Add(s.ttl).UTC()
	if s.signer != nil {
		url, err := s.signer.SignedURL(ctx, objectKey, s.ttl)
		if err != nil {
			return "", time.Time{}, err
		}
		return url, expiresAt, nil
	}
	return s.publicBaseURL + "/uploads/" + strings.TrimPrefix(objectKey, "/"), expiresAt, nil
}
