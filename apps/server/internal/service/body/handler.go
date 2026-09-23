package body

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/media"
	"github.com/zhanshimian/server/internal/repository"
)

// TaskTypeBodyOrbit 是 3D 形象 Lite 的任务类型。
const TaskTypeBodyOrbit domain.TaskType = "body_orbit"

// HandlerRepo 是 worker 侧的存储依赖。
type HandlerRepo interface {
	GetBodyOrbitWork(ctx context.Context, userID, id string) (domain.BodyPresentationInput, error)
	GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error)
	ApplyBodyOrbitResult(ctx context.Context, id, videoKey string, durationMS int, yaws []float64, keys []string, providerVersion string) error
	CommitBodyOrbit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error)
}

// ObjectStore 读写生成物（视频与抽帧）。
type ObjectStore interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Save(ctx context.Context, key string, reader io.Reader) (string, error)
	Delete(ctx context.Context, key string) error
}

// OperationProgress 上报公开操作进度（与 assessment 同一通道）。
type OperationProgress interface {
	Set(ctx context.Context, operationID string, progressBPS int, stageCode string) error
}

type Handler struct {
	repo      HandlerRepo
	objects   ObjectStore
	progress  OperationProgress
	generator Generator
	extractor media.Extractor
	logger    *slog.Logger
}

func NewHandler(repo HandlerRepo, objects ObjectStore, progress OperationProgress, generator Generator, extractor media.Extractor, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{repo: repo, objects: objects, progress: progress, generator: generator, extractor: extractor, logger: logger}
}

func (h *Handler) Type() domain.TaskType { return TaskTypeBodyOrbit }

func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	task := lease.Task
	presentationID := task.SubjectID

	work, err := h.repo.GetBodyOrbitWork(ctx, task.UserID, presentationID)
	if err != nil {
		if err == repository.ErrNotFound {
			return domainFail("body_presentation_missing"), nil
		}
		return domain.TaskResult{}, err
	}

	h.setProgress(ctx, task.OperationID, 1200, "读取照片")
	assets, err := h.repo.GetReadyAssets(ctx, task.UserID, []string{work.BodyMediaID, work.FaceMediaID})
	if err != nil {
		return domain.TaskResult{}, err
	}
	byID := make(map[string]domain.MediaAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	bodyAsset, hasBody := byID[work.BodyMediaID]
	faceAsset, hasFace := byID[work.FaceMediaID]
	if !hasBody || !hasFace {
		return domainFail("body_orbit_photos_missing"), nil
	}
	bodyBytes, err := h.readAll(ctx, bodyAsset.ObjectKey)
	if err != nil {
		return domain.TaskResult{}, err
	}
	faceBytes, err := h.readAll(ctx, faceAsset.ObjectKey)
	if err != nil {
		return domain.TaskResult{}, err
	}

	if h.generator == nil {
		return domainFail("body_orbit_not_configured"), nil
	}
	h.setProgress(ctx, task.OperationID, 4000, "生成环绕预览")
	output, err := h.generator.Generate(ctx, OrbitInput{
		Body: bodyBytes, Face: faceBytes, BodyMIME: bodyAsset.MIMEType, FaceMIME: faceAsset.MIMEType,
	})
	if err != nil {
		return domain.TaskResult{}, err
	}
	if len(output.VideoData) == 0 || output.MIMEType != "video/mp4" {
		return domainFail("body_orbit_bad_video"), nil
	}

	h.setProgress(ctx, task.OperationID, 7200, "抽出转盘静帧")
	var extracted []media.Frame
	var extractErr error
	if h.extractor != nil {
		extracted, extractErr = h.extractor.Extract(ctx, output.VideoData, output.Duration, media.OrbitFrameCount)
	} else {
		extractErr = fmt.Errorf("orbit extractor is not configured")
	}
	if extractErr != nil || len(extracted) < media.OrbitMinKeepFrames {
		h.logger.Warn("body orbit extract degraded; keeping video without frames",
			"presentation_id", presentationID, "frames", len(extracted), "error", extractErr)
		extracted = nil
	}

	h.setProgress(ctx, task.OperationID, 9500, "保存")
	videoKey := fmt.Sprintf("%s/generated/body-orbit/%s.mp4", task.UserID, presentationID)
	storedVideoKey, err := h.objects.Save(ctx, videoKey, bytes.NewReader(output.VideoData))
	if err != nil {
		return domain.TaskResult{}, err
	}
	savedKeys := []string{storedVideoKey}
	yaws := make([]float64, 0, len(extracted))
	frameKeys := make([]string, 0, len(extracted))
	for i, frame := range extracted {
		key := fmt.Sprintf("%s/generated/body-orbit/%s/%d.jpg", task.UserID, presentationID, i)
		storedKey, frameErr := h.objects.Save(ctx, key, bytes.NewReader(frame.JPEG))
		if frameErr != nil {
			h.cleanup(ctx, savedKeys)
			return domain.TaskResult{}, frameErr
		}
		savedKeys = append(savedKeys, storedKey)
		yaws = append(yaws, frame.Yaw)
		frameKeys = append(frameKeys, storedKey)
	}
	if err := h.repo.ApplyBodyOrbitResult(ctx, presentationID, storedVideoKey, int(output.Duration/time.Millisecond), yaws, frameKeys, output.ProviderVersion); err != nil {
		h.cleanup(ctx, savedKeys)
		return domain.TaskResult{}, err
	}
	return domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "body_presentation", ResultID: presentationID}, nil
}

func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	return h.repo.CommitBodyOrbit(ctx, lease, result)
}

func (h *Handler) setProgress(ctx context.Context, operationID string, bps int, stage string) {
	if h.progress == nil {
		return
	}
	_ = h.progress.Set(ctx, operationID, bps, stage)
}

func (h *Handler) readAll(ctx context.Context, key string) ([]byte, error) {
	rc, err := h.objects.Open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", key, err)
	}
	defer func() { _ = rc.Close() }()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *Handler) cleanup(ctx context.Context, keys []string) {
	for _, key := range keys {
		_ = h.objects.Delete(ctx, key)
	}
}

func domainFail(code string) domain.TaskResult {
	return domain.TaskResult{
		Disposition: domain.TaskDomainFail,
		Failure:     &domain.TaskFailure{Class: domain.ErrorPermanent, Code: code},
	}
}
