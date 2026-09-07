package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/storage"
)

type todayLookRepoStub struct {
	repository.Repository
	completeJob   domain.TodayPlanLookJob
	completeURL   string
	completeCalls int
	failJob       *domain.TodayPlanLookJob
	failCause     error
}

func (r *todayLookRepoStub) CompleteTodayPlanLook(_ context.Context, job domain.TodayPlanLookJob, resultURL, _, _ string) error {
	r.completeJob, r.completeURL = job, resultURL
	r.completeCalls++
	return nil
}

func (r *todayLookRepoStub) FailTodayPlanLook(_ context.Context, job domain.TodayPlanLookJob, cause error) error {
	r.failJob, r.failCause = &job, cause
	return nil
}

type capturingLookGenerator struct {
	output provider.LookOutput
	err    error
	input  provider.LookInput
	ctx    context.Context
}

func (g *capturingLookGenerator) Generate(ctx context.Context, input provider.LookInput) (provider.LookOutput, error) {
	g.ctx, g.input = ctx, input
	return g.output, g.err
}

type memoryStorageStub struct {
	storage.ObjectStorage
	savedKey string
}

func (s *memoryStorageStub) Save(_ context.Context, key string, _ io.Reader) (string, error) {
	s.savedKey = key
	return key, nil
}

func todayLookService(t *testing.T, generator provider.LookGenerator, repo *todayLookRepoStub) *Service {
	t.Helper()
	var logs bytes.Buffer
	return &Service{repo: repo, storage: &memoryStorageStub{}, lookGenerator: generator, logger: slog.New(slog.NewTextHandler(&logs, nil))}
}

// 今日方案生成成功后要把本人搭配图落存储并回写 completed;生成提示词必须
// 基于今日三步(发型/妆容/穿搭)与任务标识。
func TestProcessTodayPlanLookCompletesWithGeneratedImage(t *testing.T) {
	generator := &capturingLookGenerator{output: provider.LookOutput{ImageData: []byte("png-bytes"), MIMEType: "image/png", ProviderVersion: "stub-look-v1"}}
	repo := &todayLookRepoStub{}
	service := todayLookService(t, generator, repo)
	job := domain.TodayPlanLookJob{PlanID: "today-1", ReportID: "report-1", UserID: "user-1", Title: "休息日·利落黑调微整", Summary: "肩线拉合身",
		Steps: []domain.TodayPlanStep{{Category: "hair", Title: "重心后移", Copy: "头顶发根梳向后面"}, {Category: "outfit", Title: "肩线归位", Copy: "选合肩版型"}},
		MediaIDs: []string{"media-1"}}
	service.processTodayPlanLook(context.Background(), job)
	if repo.completeCalls != 1 || repo.completeJob.PlanID != "today-1" {
		t.Fatalf("completion was not recorded: calls=%d job=%#v", repo.completeCalls, repo.completeJob)
	}
	if !strings.HasPrefix(repo.completeURL, "/uploads/") || !strings.HasSuffix(repo.completeURL, ".png") {
		t.Fatalf("unexpected completed url %q", repo.completeURL)
	}
	if !strings.Contains(repo.completeURL, "/generated/today/") {
		t.Fatalf("expected today storage prefix, got %q", repo.completeURL)
	}
	if generator.input.Name != job.Title || len(generator.input.MediaIDs) != 1 || len(generator.input.Steps) != 2 || generator.input.Steps[1].Summary != "选合肩版型" {
		t.Fatalf("look input lost today-plan grounding: %#v", generator.input)
	}
	if provider.InvocationSource(generator.ctx) != "today_look:today-1" {
		t.Fatalf("invocation source missing from generator context: %q", provider.InvocationSource(generator.ctx))
	}
}

// 生成失败要走 FailTodayPlanLook,日志标记还会重试;任务标识必须一路传下去。
func TestProcessTodayPlanLookFailsAndSchedulesRetry(t *testing.T) {
	generator := &capturingLookGenerator{err: errors.New("provider down")}
	repo := &todayLookRepoStub{}
	service := todayLookService(t, generator, repo)
	var logs bytes.Buffer
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	service.processTodayPlanLook(context.Background(), domain.TodayPlanLookJob{PlanID: "today-1", Attempt: 1})
	if repo.failJob == nil || repo.failJob.PlanID != "today-1" || !strings.Contains(repo.failCause.Error(), "provider down") {
		t.Fatalf("failure was not recorded: job=%#v cause=%v", repo.failJob, repo.failCause)
	}
	if !strings.Contains(logs.String(), "today plan look job failed, retry scheduled") {
		t.Fatalf("expected retry WARN, got: %s", logs.String())
	}
	if provider.InvocationSource(generator.ctx) != "today_look:today-1" {
		t.Fatalf("invocation source missing from generator context: %q", provider.InvocationSource(generator.ctx))
	}
}
