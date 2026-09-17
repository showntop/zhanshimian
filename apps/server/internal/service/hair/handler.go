package hair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

// TaskTypeHairPreview 是发型预览的任务类型。
const TaskTypeHairPreview domain.TaskType = "hair_preview"

// TaskPayloadVersion 是 hair_preview 任务载荷版本。
const TaskPayloadVersion = 1

// 阶段码与渲染同一命名风格，公开 Operation 轮询据此展示进度。
const (
	StageLoading    = "hair.loading"
	StageGenerating = "hair.generating"
	StageSaving     = "hair.saving"
)

// 失败码：transient 走任务重试（重试耗尽后由 runner 以 TaskDomainFail 收尾），
// quality_rejected/permanent 直接 domain fail 终态。
const (
	codePreviewMissing        = "hair_preview_missing"
	codeCapabilityUnavailable = "hair_capability_unavailable"
	codeSourceUnavailable     = "hair_source_unavailable"
	codeGenerateFailed        = "hair_generate_failed"
	codeOutputInvalid         = "hair_output_invalid"
	codeResultStoreFailed     = "hair_result_store_failed"
)

// HandlerRepo 是 worker 侧的存储依赖。
type HandlerRepo interface {
	GetHairPreviewWork(ctx context.Context, userID string, previewID string) (PreviewWork, error)
	MarkHairPreviewGenerating(ctx context.Context, userID string, previewID string) error
	ApplyHairPreviewResult(ctx context.Context, userID string, previewID string, result AppliedResult) error
	CommitHairPreview(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error)
}

// PreviewWork 是一次生成的取件：预览状态 + 发型 + 源图对象引用。
type PreviewWork struct {
	State           string
	StyleID         string
	StyleName       string
	SourceObjectKey string
	SourceMIMEType  string
}

// AppliedResult 是入库的生成图事实（媒体资产 + 对象引用 + 台账）。
type AppliedResult struct {
	ObjectKey            string
	SHA256               string
	MIMEType             string
	ByteSize             int64
	Width                int
	Height               int
	ProviderInvocationID string
}

// ObjectStore 读写生成物（与 body 同一窄口）。
type ObjectStore interface {
	Save(ctx context.Context, key string, reader io.Reader) (string, error)
	Delete(ctx context.Context, key string) error
}

// SourceLoader 读源图字节并按编辑预算约束（实现挂在 bootstrap，
// 与 assessment 的 imageLoader 同一做法：数据万象下载时压缩 + 本地预算兜底，
// 原图内联帧会撑爆生成请求体与超时）。
type SourceLoader interface {
	Load(ctx context.Context, work PreviewWork) (SourceImage, error)
}

// OperationProgress 上报公开操作进度（与 assessment/body 同一通道）。
type OperationProgress interface {
	Set(ctx context.Context, operationID string, progressBPS int, stageCode string) error
}

// Generator 是 hair_edit 能力路由的窄口：一次编辑调用，不含 fallback 循环。
type Generator interface {
	Generate(ctx context.Context, input GenerateInput) (GenerateOutput, error)
}

type GenerateInput struct {
	Prompt string
	Face   SourceImage
}

type SourceImage struct {
	MIMEType string
	Data     []byte
}

type GenerateOutput struct {
	Data         []byte
	MIMEType     string
	InvocationID string
}

// NormalizedImage 是归一化后的干净 JPEG（无 provider 元数据）。
type NormalizedImage struct {
	Data     []byte
	MIMEType string
	SHA256   string
	ByteSize int64
	Width    int
	Height   int
}

// ImageNormalizer 把 provider 输出归一化为可发布 JPEG。
type ImageNormalizer interface {
	Normalize(data []byte, declaredMIME string) (NormalizedImage, error)
}

type Handler struct {
	repo       HandlerRepo
	objects    ObjectStore
	sources    SourceLoader
	progress   OperationProgress
	generator  Generator
	normalizer ImageNormalizer
}

func NewHandler(repo HandlerRepo, objects ObjectStore, sources SourceLoader, progress OperationProgress, generator Generator, normalizer ImageNormalizer) *Handler {
	return &Handler{repo: repo, objects: objects, sources: sources, progress: progress, generator: generator, normalizer: normalizer}
}

func (h *Handler) Type() domain.TaskType { return TaskTypeHairPreview }

// Execute 生成发型预览：取件 → 状态守卫 → 读源图（编辑预算约束）→
// hair_edit 生成 → JPEG 归一化 → 存对象 → 落结果行（state=ready）。
// 任务/操作终态由 Commit 在租约 CAS 下翻转。
func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	task := lease.Task
	var payload domain.HairPreviewTaskPayload
	if err := decodeTaskPayload(task.Payload, &payload); err != nil {
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: "hair_payload_invalid"}
	}
	previewID := payload.PreviewID
	if previewID == "" {
		previewID = task.SubjectID
	}

	work, err := h.repo.GetHairPreviewWork(ctx, task.UserID, previewID)
	if errors.Is(err, repository.ErrNotFound) {
		return domainFail(domain.ErrorPermanent, codePreviewMissing), nil
	}
	if err != nil {
		return domain.TaskResult{}, err
	}
	switch work.State {
	case StateQueued, StateGenerating, StateChecking:
	default:
		// 已终态（重复领取/崩溃恢复）：不再生成，任务直接 supersede，
		// 公开 Operation 保持既有终态。
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorSuperseded, Code: "hair_preview_terminal"}
	}

	h.setProgress(ctx, task.OperationID, 1500, StageLoading)
	face, err := h.loadSource(ctx, work)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if h.generator == nil {
		return domainFail(domain.ErrorPermanent, codeCapabilityUnavailable), nil
	}
	if err := h.repo.MarkHairPreviewGenerating(ctx, task.UserID, previewID); err != nil {
		return domain.TaskResult{}, err
	}
	h.setProgress(ctx, task.OperationID, 4000, StageGenerating)

	generated, err := h.generator.Generate(ctx, GenerateInput{Prompt: previewPrompt(work), Face: face})
	if err != nil {
		// 能力路由的调用失败（网络/限流/超时/厂商 5xx）按瞬时分类，
		// 交给 task runner 在重试预算内补跑。
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: codeGenerateFailed}
	}
	normalized, err := h.normalizer.Normalize(generated.Data, generated.MIMEType)
	if err != nil {
		// provider 输出不合图像契约：重跑同一源图大概率同样失败，按质量
		// 拒绝落域失败（用户可重新发起），不消耗任务重试预算。
		return domainFail(domain.ErrorQualityRejected, codeOutputInvalid), nil
	}

	h.setProgress(ctx, task.OperationID, 8000, StageSaving)
	objectKey := fmt.Sprintf("%s/generated/hair-preview/%s.jpg", task.UserID, previewID)
	storedKey, err := h.objects.Save(ctx, objectKey, bytes.NewReader(normalized.Data))
	if err != nil {
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: codeResultStoreFailed}
	}
	if err := h.repo.ApplyHairPreviewResult(ctx, task.UserID, previewID, AppliedResult{
		ObjectKey:            storedKey,
		SHA256:               normalized.SHA256,
		MIMEType:             normalized.MIMEType,
		ByteSize:             normalized.ByteSize,
		Width:                normalized.Width,
		Height:               normalized.Height,
		ProviderInvocationID: generated.InvocationID,
	}); err != nil {
		_ = h.objects.Delete(ctx, storedKey)
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: codeResultStoreFailed}
	}
	return domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "hair_preview", ResultID: previewID}, nil
}

// Commit 统一在写总前补公开文案与重试标记（runner 合成的 transient 重试耗尽、
// panic/timeout、permanent 上抛不经过 Execute 的 domainFail）。
func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	if result.Disposition == domain.TaskDomainFail && result.Failure != nil && result.Failure.PublicMessage == "" {
		enrichTaskFailure(result.Failure)
	}
	return h.repo.CommitHairPreview(ctx, lease, result)
}

func (h *Handler) setProgress(ctx context.Context, operationID string, bps int, stage string) {
	if h.progress == nil {
		return
	}
	_ = h.progress.Set(ctx, operationID, bps, stage)
}

// loadSource 读正脸原图（编辑预算约束由 SourceLoader 实现负责）。
// 对象库读失败按 transient 分类，交给 task runner 重试。
func (h *Handler) loadSource(ctx context.Context, work PreviewWork) (SourceImage, error) {
	if work.SourceObjectKey == "" {
		return SourceImage{}, &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: codeSourceUnavailable}
	}
	image, err := h.sources.Load(ctx, work)
	if err != nil {
		return SourceImage{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: codeSourceUnavailable}
	}
	return image, nil
}

// previewPrompt 只描述发型变化与保持项：不打分、不评判五官/身材（红线 1）。
// 输出比例显式要 3:4 竖构图——发型卡/历史/风格卡全是竖框；模型不听话也有
// 归一化层的 3:4 裁切兜底（hairNormalizer），两层说的是同一条显示规范。
func previewPrompt(work PreviewWork) string {
	const aspectLine = "输出一张 3:4 竖构图照片，人物头顶留少量空间。"
	if work.StyleName != "" {
		return fmt.Sprintf("把照片中人物的发型换成「%s」，只改变发型；人物的面部特征、表情、妆容、服装与背景保持完全不变，效果自然真实。%s", work.StyleName, aspectLine)
	}
	return "为照片中的人物换一个自然好看的新发型，只改变发型；人物的面部特征、表情、妆容、服装与背景保持完全不变，效果自然真实。" + aspectLine
}

func domainFail(class domain.ErrorClass, code string) domain.TaskResult {
	failure := &domain.TaskFailure{Class: class, Code: code}
	enrichTaskFailure(failure)
	return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: failure}
}

// classifyTaskFailure 把内部失败码翻译成对客户端公开的文案与重试标记，
// 不泄露内部细节（厂商、模型、堆栈）。
func classifyTaskFailure(class domain.ErrorClass, code string) (message string, retryable bool) {
	switch class {
	case domain.ErrorTransient, domain.ErrorThrottled:
		// 瞬时故障重试耗尽（如 AI 通道故障）：照片本身没问题，可重新发起。
		return "预览生成暂时未完成，请稍后重试", true
	case domain.ErrorQualityRejected:
		return "这次没能生成可靠的预览，请再试一次", true
	default:
		return "预览生成失败，请重新发起", false
	}
}

func enrichTaskFailure(failure *domain.TaskFailure) {
	failure.PublicMessage, failure.Retryable = classifyTaskFailure(failure.Class, failure.Code)
}

func decodeTaskPayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return errors.New("empty task payload")
	}
	return json.Unmarshal(raw, target)
}

var _ taskrunner.Handler = (*Handler)(nil)
