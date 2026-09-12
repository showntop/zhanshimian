package service

import (
	"context"
	"errors"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// HealthJobs exposes the unified-queue health numbers for /healthz.
func (s *Service) HealthJobs(ctx context.Context) (domain.JobsHealth, error) {
	return s.repo.HealthJobs(ctx)
}

// HomeBootstrap assembles the whole first screen in one pass: profile summary,
// latest report, today's plan, the user's live tasks and the most recent plan.
// Missing pieces stay nil so the client renders its onboarding states.
func (s *Service) HomeBootstrap(ctx context.Context, user domain.User) (domain.HomeBootstrap, error) {
	bootstrap := domain.HomeBootstrap{ActiveTasks: []domain.TaskView{}}
	if profile, err := s.repo.GetUserProfile(ctx, user.ID); err == nil {
		bootstrap.ProfileSummary = &profile
	} else if !errors.Is(err, repository.ErrNotFound) {
		return bootstrap, err
	}
	if report, err := s.repo.LatestReport(ctx, user.ID); err == nil {
		report.CurrentImageURL = s.resolveAssetURL(report.CurrentImageURL)
		bootstrap.Report = &report
	} else if !errors.Is(err, repository.ErrNotFound) {
		return bootstrap, err
	}
	if today, err := s.repo.GetTodayPlan(ctx, user.ID); err == nil {
		s.hydrateTodayPlan(ctx, user.ID, &today)
		bootstrap.TodayPlan = &today
	} else if !errors.Is(err, repository.ErrNotFound) {
		return bootstrap, err
	}
	tasks, err := s.repo.ActiveTasks(ctx, user.ID, 10)
	if err != nil {
		return bootstrap, err
	}
	bootstrap.ActiveTasks = viewTasks(tasks)
	if summary, err := s.BillingSummary(ctx, user.ID); err == nil {
		bootstrap.Billing = &summary
	}
	if plan, err := s.repo.RecentPlan(ctx, user.ID); err == nil {
		if err := s.attachPlanLookTasks(ctx, user.ID, []domain.Plan{plan}); err == nil {
			s.resolvePlanURLs(&plan)
			bootstrap.RecentPlan = &plan
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return bootstrap, err
	}
	return bootstrap, nil
}
