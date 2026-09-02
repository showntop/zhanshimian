package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
)

// failWriteRecorder captures the state of the context used by the
// failure/rejection write-backs at the moment they are invoked.
type failWriteRecorder struct {
	repository.Repository
	failErr           error
	failDeadline      time.Time
	failHasDeadline   bool
	rejectErr         error
	rejectDeadline    time.Time
	rejectHasDeadline bool
}

func (r *failWriteRecorder) FailAnalysis(ctx context.Context, _ domain.AnalysisJob, _ error) error {
	r.failErr = ctx.Err()
	r.failDeadline, r.failHasDeadline = ctx.Deadline()
	return nil
}

func (r *failWriteRecorder) RejectAnalysis(ctx context.Context, _ domain.AnalysisJob, _ string) error {
	r.rejectErr = ctx.Err()
	r.rejectDeadline, r.rejectHasDeadline = ctx.Deadline()
	return nil
}

// errAnalyzer returns a fixed error without consulting ctx.
type errAnalyzer struct{ err error }

func (a errAnalyzer) Analyze(context.Context, domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	return domain.AnalysisOutput{}, a.err
}

// deadlineAnalyzer blocks until the job context is cancelled, mimicking a
// provider call that returns only after the 5 minute job deadline fires.
type deadlineAnalyzer struct{}

func (deadlineAnalyzer) Analyze(ctx context.Context, _ domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	<-ctx.Done()
	return domain.AnalysisOutput{}, ctx.Err()
}

func assertFreshFailContext(t *testing.T, err error, deadline time.Time, hasDeadline bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("failure write-back reused the expired job context: %v", err)
	}
	if !hasDeadline {
		t.Fatal("failure write-back context carries no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("failure write-back context has unexpected deadline: %s", remaining)
	}
}

// 回归：provider 超时后 jobCtx 已过期，失败回写必须换一个未过期的短超时
// 上下文，否则任务永远停在 running/processing。
func TestProcessJobFailWriteUsesFreshContext(t *testing.T) {
	repo := &failWriteRecorder{}
	service := &Service{repo: repo, analyzer: deadlineAnalyzer{}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	service.processJob(ctx, domain.AnalysisJob{ID: "job-1", AnalysisID: "analysis-1", UserID: "user-1"})
	assertFreshFailContext(t, repo.failErr, repo.failDeadline, repo.failHasDeadline)
}

// 同上：照片被拒（RejectAnalysis 路径）也不能用已过期的 jobCtx 回写。
func TestProcessJobRejectWriteUsesFreshContext(t *testing.T) {
	repo := &failWriteRecorder{}
	service := &Service{repo: repo, analyzer: errAnalyzer{err: &provider.PhotoRejectedError{}}}
	service.processJob(context.Background(), domain.AnalysisJob{ID: "job-1", AnalysisID: "analysis-1", UserID: "user-1"})
	assertFreshFailContext(t, repo.rejectErr, repo.rejectDeadline, repo.rejectHasDeadline)
}
