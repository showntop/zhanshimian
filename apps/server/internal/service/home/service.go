package home

import (
	"context"
	"errors"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// Clock 是时间源的最小抽象：测试注入固定时钟，生产注入系统时钟。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// NewClock 返回生产用的系统时钟。
func NewClock() Clock { return systemClock{} }

// Dependencies 是首页聚合的领域读端口：任一端口为 nil 时对应键整体省略，
// 便于测试与降级组装；生产环境全部接线。
type Dependencies struct {
	Reports  ReportReader
	PlanSets PlanSetReader
	Today    TodayReader
	Billing  BillingReader
	Media    MediaSigner
}

type Service struct {
	reader Reader
	clock  Clock
	deps   Dependencies
}

func New(reader Reader, clock Clock, deps Dependencies) *Service {
	return &Service{reader: reader, clock: clock, deps: deps}
}

// Bootstrap 组装首页一屏：SQL 读模型供给档案/进行中操作，报告/方案集/
// 今日/权益分别复用各领域的公开读路径。任一内容键不存在时省略该键，
// 读路径本身出错则整请求失败（不给客户端半份坏数据）。
func (s *Service) Bootstrap(ctx context.Context, userID string) (Bootstrap, error) {
	out := Bootstrap{ActiveOperations: []domain.OperationRef{}}

	base, err := s.reader.ReadHome(ctx, userID, s.clock.Now())
	if err != nil {
		return out, err
	}
	// 评估发布会回填仅含 current_report_id 的空档案行；视为「从未填写」，
	// 与 GET /v1/me/profile 的判空规则一致（契约 height_cm 最小 100）。
	if base.Profile != nil && (base.Profile.Role != "" || base.Profile.HeightCM != 0) {
		out.ProfileSummary = base.Profile
	}
	if base.ActiveOperations != nil {
		out.ActiveOperations = base.ActiveOperations
	}

	if s.deps.Reports != nil {
		report, err := s.deps.Reports.GetCurrentReport(ctx, userID)
		switch {
		case err == nil:
			view := newReportDTO(report)
			out.Report = &view
		case errors.Is(err, repository.ErrNotFound):
		default:
			return out, err
		}
	}

	if s.deps.PlanSets != nil {
		planSet, err := s.latestPlanSet(ctx, userID)
		if err != nil {
			return out, err
		}
		out.PlanSet = planSet
	}

	if s.deps.Today != nil {
		plan, err := s.deps.Today.Current(ctx, userID)
		switch {
		case err == nil:
			if err := s.signTodayMedia(ctx, userID, &plan); err != nil {
				return out, err
			}
			out.TodayPlan = &plan
		case errors.Is(err, repository.ErrNotFound):
		default:
			return out, err
		}
	}

	if s.deps.Billing != nil {
		summary, err := s.deps.Billing.BillingSummary(ctx, userID)
		if err != nil {
			return out, err
		}
		if summary.SKUs == nil {
			summary.SKUs = []domain.BillingSKU{}
		}
		out.Billing = &summary
	}

	return out, nil
}

// latestPlanSet 取最近一个已发布方案集：ID 由读模型定位，领域对象经
// planning 的 GetPlanSet（含变体，各 variant 的当前渲染视图与签名媒体
// 已由 planning 读模型合并）。无方案集时返回 nil, nil。
func (s *Service) latestPlanSet(ctx context.Context, userID string) (*planSetDTO, error) {
	id, err := s.reader.LatestPublishedPlanSetID(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	planSet, err := s.deps.PlanSets.GetPlanSet(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := newPlanSetDTO(planSet)
	return &view, nil
}

// signTodayMedia 给今日方案的发布媒体补可读 URL：对象定位来自读模型，
// 签名经 MediaSigner；媒体行已删时整段置空（不发坏图）。
func (s *Service) signTodayMedia(ctx context.Context, userID string, plan *TodayPlan) error {
	if plan.Media == nil || s.deps.Media == nil {
		return nil
	}
	object, err := s.reader.MediaObjectInfo(ctx, userID, plan.Media.AssetID)
	if errors.Is(err, repository.ErrNotFound) {
		plan.Media = nil
		return nil
	}
	if err != nil {
		return err
	}
	url, expiresAt, err := s.deps.Media.SignedURL(ctx, object.ObjectKey)
	if err != nil {
		return err
	}
	plan.Media.URL = url
	plan.Media.URLExpiresAt = expiresAt
	if plan.Media.MIMEType == "" {
		plan.Media.MIMEType = object.MIMEType
	}
	return nil
}
