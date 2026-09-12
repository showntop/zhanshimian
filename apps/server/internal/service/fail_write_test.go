package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
)

var errRetry = errors.New("provider unavailable")

// taskWriteRecorder 捕获统一任务系统回写发生瞬间的上下文与参数。
type taskWriteRecorder struct {
	repository.Repository
	getInputErr error

	failErr         error
	failDeadline    time.Time
	failHasDeadline bool
	failCode        string
	failRetryAt     time.Time

	completeCalled bool

	presentationErr         error
	presentationDeadline    time.Time
	presentationHasDeadline bool
}

func (r *taskWriteRecorder) GetAnalysisInput(context.Context, string, string) (domain.CreateAnalysisInput, error) {
	if r.getInputErr != nil {
		return domain.CreateAnalysisInput{}, r.getInputErr
	}
	return domain.CreateAnalysisInput{Scene: "interview"}, nil
}

func (r *taskWriteRecorder) UpdateTaskProgress(context.Context, string, int, string) error {
	return nil
}

func (r *taskWriteRecorder) UpdateAnalysisProgress(context.Context, string, int, string) error {
	return nil
}

func (r *taskWriteRecorder) FailTask(ctx context.Context, _, code, _ string, _ []string, retryAt time.Time) error {
	r.failErr = ctx.Err()
	r.failDeadline, r.failHasDeadline = ctx.Deadline()
	r.failCode = code
	r.failRetryAt = retryAt
	return nil
}

func (r *taskWriteRecorder) CompleteTask(context.Context, string, string) error {
	r.completeCalled = true
	return nil
}

func (r *taskWriteRecorder) RefundBilling(context.Context, string, string) error { return nil }

func (r *taskWriteRecorder) FailAnalysisPresentation(ctx context.Context, _, _, _ string) error {
	r.presentationErr = ctx.Err()
	r.presentationDeadline, r.presentationHasDeadline = ctx.Deadline()
	return nil
}

// deadlineAnalyzer 阻塞到 jobCtx 取消：模拟 provider 调满 5 分钟任务超时后才返回。
type deadlineAnalyzer struct{}

func (deadlineAnalyzer) Analyze(ctx context.Context, _ domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	<-ctx.Done()
	return domain.AnalysisOutput{}, ctx.Err()
}

type errAnalyzer struct{ err error }

func (a errAnalyzer) Analyze(context.Context, domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	return domain.AnalysisOutput{}, a.err
}

// delayAnalyzer 先耗过 failWriteTimeout，再返回错误。用来抓住
// 「Analyze 开始时就开 failContext」导致分析行回写 DeadlineExceeded。
type delayAnalyzer struct {
	delay time.Duration
	err   error
}

func (a delayAnalyzer) Analyze(ctx context.Context, _ domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	timer := time.NewTimer(a.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return domain.AnalysisOutput{}, ctx.Err()
	case <-timer.C:
		return domain.AnalysisOutput{}, a.err
	}
}

func newTaskTestService(repo repository.Repository, analyzer provider.Analyzer, logger *slog.Logger) *Service {
	svc := &Service{repo: repo, analyzer: analyzer, logger: logger}
	svc.handlers = map[domain.TaskType]TaskHandler{
		domain.TaskTypeAnalysis: analysisTaskHandler{svc},
	}
	return svc
}

func analysisTask(attempts int) domain.Task {
	payload, _ := json.Marshal(domain.AnalysisTaskPayload{AnalysisID: "analysis-1"})
	return domain.Task{
		ID: "task-1", UserID: "user-1", Type: string(domain.TaskTypeAnalysis),
		Attempts: attempts, Payload: payload,
	}
}

func assertFreshFailContext(t *testing.T, err error, deadline time.Time, hasDeadline bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("失败回写复用了已过期的 job 上下文: %v", err)
	}
	if !hasDeadline {
		t.Fatal("失败回写上下文缺少 deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("失败回写上下文 deadline 异常: %s", remaining)
	}
}

// 回归：provider 超时后 jobCtx 已过期，统一任务的失败回写必须换用独立的
// failContext，否则任务永远停在 processing。
func TestRunClaimedTaskFailWriteUsesFreshContext(t *testing.T) {
	repo := &taskWriteRecorder{}
	svc := newTaskTestService(repo, deadlineAnalyzer{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	svc.runClaimedTask(ctx, analysisTask(1))
	assertFreshFailContext(t, repo.failErr, repo.failDeadline, repo.failHasDeadline)
	if !repo.failRetryAt.After(time.Now()) {
		t.Fatalf("瞬时失败应带退避 retryAt，实际: %v", repo.failRetryAt)
	}
}

// 照片被拒（photo_rejected 永久失败路径）同样不能用已过期的 jobCtx 回写。
func TestRunClaimedTaskPhotoRejectUsesFreshContext(t *testing.T) {
	repo := &taskWriteRecorder{}
	svc := newTaskTestService(repo, errAnalyzer{err: &provider.PhotoRejectedError{Rejections: []provider.PhotoRejection{
		{Kind: "face", Reason: "光线过暗"},
	}}}, nil)
	svc.runClaimedTask(context.Background(), analysisTask(1))
	assertFreshFailContext(t, repo.failErr, repo.failDeadline, repo.failHasDeadline)
	if repo.failCode != taskErrorCodePhotoRejected {
		t.Fatalf("期望 photo_rejected 错误码，实际: %q", repo.failCode)
	}
	if !repo.failRetryAt.IsZero() {
		t.Fatalf("永久失败不应设置 retryAt，实际: %v", repo.failRetryAt)
	}
	assertFreshFailContext(t, repo.presentationErr, repo.presentationDeadline, repo.presentationHasDeadline)
}

// 回归：photo_check / 分析调用经常超过 10 秒。分析行失败回写若在
// Analyze 开始时就开 failContext，返回时 ctx 已过期，SQL 写不进去，
// 任务已 failed、分析行仍 processing，用户看到读路径兜底的超时文案。
func TestProcessAnalysisPresentationWriteSurvivesLongAnalyze(t *testing.T) {
	prev := failWriteTimeout
	failWriteTimeout = 25 * time.Millisecond
	t.Cleanup(func() { failWriteTimeout = prev })

	repo := &taskWriteRecorder{}
	svc := newTaskTestService(repo, delayAnalyzer{delay: 40 * time.Millisecond, err: errRetry}, nil)
	svc.runClaimedTask(context.Background(), analysisTask(taskMaxAttempts[domain.TaskTypeAnalysis]))
	assertFreshFailContext(t, repo.presentationErr, repo.presentationDeadline, repo.presentationHasDeadline)
}

func TestProcessAnalysisPhotoRejectWriteSurvivesLongAnalyze(t *testing.T) {
	prev := failWriteTimeout
	failWriteTimeout = 25 * time.Millisecond
	t.Cleanup(func() { failWriteTimeout = prev })

	repo := &taskWriteRecorder{}
	svc := newTaskTestService(repo, delayAnalyzer{
		delay: 40 * time.Millisecond,
		err: &provider.PhotoRejectedError{Rejections: []provider.PhotoRejection{
			{Kind: "face", Reason: "光线过暗"},
		}},
	}, nil)
	svc.runClaimedTask(context.Background(), analysisTask(1))
	assertFreshFailContext(t, repo.presentationErr, repo.presentationDeadline, repo.presentationHasDeadline)
	if repo.failCode != taskErrorCodePhotoRejected {
		t.Fatalf("期望 photo_rejected 错误码，实际: %q", repo.failCode)
	}
}

// 任务失败必须留下任务级日志：未达重试上限打 WARN（带 attempt 与退避），
// 达到上限或永久错误打 ERROR。
func TestFinishTaskLogsByAttempt(t *testing.T) {
	repo := &taskWriteRecorder{}
	var logs bytes.Buffer
	svc := newTaskTestService(repo, nil, slog.New(slog.NewTextHandler(&logs, nil)))

	svc.finishTask(analysisTask(1), errRetry)
	if !strings.Contains(logs.String(), "task failed, retry scheduled") || !strings.Contains(logs.String(), "attempt=1") {
		t.Fatalf("期望可重试 WARN 带 attempt，实际: %s", logs.String())
	}
	if repo.failCode != "" {
		t.Fatalf("可重试失败不应带终态错误码，实际: %q", repo.failCode)
	}
	logs.Reset()
	repo.failRetryAt = time.Time{}

	svc.finishTask(analysisTask(taskMaxAttempts[domain.TaskTypeAnalysis]), errRetry)
	if !strings.Contains(logs.String(), "task failed permanently") {
		t.Fatalf("期望终态 ERROR，实际: %s", logs.String())
	}
	if strings.Contains(logs.String(), "retry scheduled") {
		t.Fatalf("终态失败不得记录为可重试: %s", logs.String())
	}
	if !repo.failRetryAt.IsZero() {
		t.Fatalf("终态失败不应设置 retryAt，实际: %v", repo.failRetryAt)
	}
}

// 永久性 provider 错误（401/403/配额/契约违规）不重试。
func TestFinishTaskPermanentProviderErrorStopsRetrying(t *testing.T) {
	repo := &taskWriteRecorder{}
	svc := newTaskTestService(repo, nil, nil)
	permanent := []string{
		"aliyun returned status 403: AccessDenied.Unpurchased",
		"volcengine returned status 401: AuthenticationError",
		"generated image has unsupported image format image/gif",
		"image response contained no usable image",
	}
	for _, message := range permanent {
		repo.failRetryAt = time.Time{}
		svc.finishTask(analysisTask(1), errors.New(message))
		if !repo.failRetryAt.IsZero() {
			t.Fatalf("永久错误不应重试: %q", message)
		}
	}
	transient := errors.New("aliyun request: context deadline exceeded")
	repo.failRetryAt = time.Time{}
	svc.finishTask(analysisTask(1), transient)
	if repo.failRetryAt.IsZero() {
		t.Fatal("瞬时 provider 错误应重试")
	}
}
