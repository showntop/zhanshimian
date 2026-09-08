package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/storage"
)

type todayLookRepoStub struct {
	repository.Repository
	jobErr   error
	job      domain.TodayPlanLookJob
	applied  bool
	appliedURL string
	appliedVersion string
}

func (r *todayLookRepoStub) GetTodayPlanLookJob(_ context.Context, _, _ string) (domain.TodayPlanLookJob, error) {
	if r.jobErr != nil {
		return domain.TodayPlanLookJob{}, r.jobErr
	}
	return r.job, nil
}

func (r *todayLookRepoStub) ApplyTodayLookResult(_ context.Context, _ string, resultURL, _, providerVersion string) error {
	r.applied = true
	r.appliedURL = resultURL
	r.appliedVersion = providerVersion
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

func todayTask(planID string, attempts int) domain.Task {
	payload, _ := json.Marshal(domain.TodayLookTaskPayload{PlanID: planID})
	return domain.Task{
		ID: "task-today-1", UserID: "user-1", Type: string(domain.TaskTypeTodayLook),
		Attempts: attempts, Payload: payload,
	}
}

// 今日方案生成成功后要把本人搭配图落存储并回写结果；生成提示词必须基于
// 今日三步（发型/妆容/穿搭）并携带任务标识。
func TestProcessTodayLookCompletesWithGeneratedImage(t *testing.T) {
	generator := &capturingLookGenerator{output: provider.LookOutput{ImageData: []byte("png-bytes"), MIMEType: "image/png", ProviderVersion: "stub-look-v1"}}
	store := &memoryStorageStub{}
	repo := &todayLookRepoStub{job: domain.TodayPlanLookJob{PlanID: "today-1", ReportID: "report-1", UserID: "user-1", Title: "休息日·利落黑调微整", Summary: "肩线拉合身",
		Steps: []domain.TodayPlanStep{{Category: "hair", Title: "重心后移", Copy: "头顶发根梳向后面"}, {Category: "outfit", Title: "肩线归位", Copy: "选合肩版型"}},
		MediaIDs: []string{"media-1"}}}
	service := &Service{repo: repo, storage: store, lookGenerator: generator}

	resultRef, err := service.processTodayLook(context.Background(), todayTask("today-1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if resultRef != "today-1" {
		t.Fatalf("result ref should reference the today plan: %q", resultRef)
	}
	if !repo.applied {
		t.Fatal("生成结果未回写")
	}
	if !contains(repo.appliedURL, "/uploads/") || !hasSuffix(repo.appliedURL, ".png") || !contains(repo.appliedURL, "/generated/today/") {
		t.Fatalf("unexpected applied url %q", repo.appliedURL)
	}
	if repo.appliedVersion != "stub-look-v1" {
		t.Fatalf("provider version lost: %q", repo.appliedVersion)
	}
	if generator.input.Name != repo.job.Title || len(generator.input.MediaIDs) != 1 || len(generator.input.Steps) != 2 || generator.input.Steps[1].Summary != "选合肩版型" {
		t.Fatalf("look input lost today-plan grounding: %#v", generator.input)
	}
	if provider.InvocationSource(generator.ctx) != "today_look:today-1" {
		t.Fatalf("invocation source missing from generator context: %q", provider.InvocationSource(generator.ctx))
	}
}

// 生成失败必须把错误上抛给统一任务循环（由 finishTask 决定重试/终态），
// 绝不落任何结果；任务标识必须一路传下去。
func TestProcessTodayLookSurfacesGeneratorFailure(t *testing.T) {
	generator := &capturingLookGenerator{err: errors.New("provider down")}
	repo := &todayLookRepoStub{job: domain.TodayPlanLookJob{PlanID: "today-1", Attempt: 1}}
	service := &Service{repo: repo, storage: &memoryStorageStub{}, lookGenerator: generator}

	if _, err := service.processTodayLook(context.Background(), todayTask("today-1", 1)); err == nil || !contains(err.Error(), "provider down") {
		t.Fatalf("generator failure must surface, got: %v", err)
	}
	if repo.applied {
		t.Fatal("failed generation must not be applied")
	}
	if provider.InvocationSource(generator.ctx) != "today_look:today-1" {
		t.Fatalf("invocation source missing from generator context: %q", provider.InvocationSource(generator.ctx))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
