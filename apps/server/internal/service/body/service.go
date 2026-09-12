// Package body 实现 3D 形象 Lite：创建环绕展示、读启动状态、worker 结果落库。
// 状态/进度不做冗余存储，一律投影自关联的 body_orbit 任务。
package body

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

var (
	// ErrValidation 输入不满足「正脸 + 正面全身」。
	ErrValidation = errors.New("body validation failed")
	// ErrCapabilityUnavailable 未配置 body_orbit 能力（路由未开放）。
	ErrCapabilityUnavailable = errors.New("body_orbit capability unavailable")
)

// Generator 是环绕视频生成器（Demo 夹具或真实供应商），由路由配置决定是否可用。
type Generator interface {
	Generate(ctx context.Context, input OrbitInput) (OrbitOutput, error)
}

// OrbitInput/OrbitOutput 与 provider 层解耦的最小形状。
type OrbitInput struct {
	Body, Face         []byte
	BodyMIME, FaceMIME string
}

type OrbitOutput struct {
	VideoData       []byte
	MIMEType        string
	DurationMS      int
	ProviderVersion string
}

type Service struct {
	repo        Repository
	assets      AssetReader
	signer      URLSigner
	billing     Billing
	generator   Generator
	maxAttempts int
}

func New(repo Repository, assets AssetReader, signer URLSigner, billing Billing, generator Generator, maxAttempts int) *Service {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &Service{repo: repo, assets: assets, signer: signer, billing: billing, generator: generator, maxAttempts: maxAttempts}
}

// Available 报告 body_orbit 能力是否已配置（实验室启动卡用）。
func (s *Service) Available() bool { return s.generator != nil }

// Create 校验输入媒体、创建展示资源与任务、按次预扣额度。
// 命中进行中任务时复用既有资源，不重复扣费。
func (s *Service) Create(ctx context.Context, userID string, input domain.BodyPresentationInput) (domain.CreatedBodyPresentation, error) {
	if s.generator == nil {
		return domain.CreatedBodyPresentation{}, fmt.Errorf("%w: 3D 形象暂未开放", ErrCapabilityUnavailable)
	}
	if _, err := uuid.Parse(input.BodyMediaID); err != nil {
		return domain.CreatedBodyPresentation{}, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}
	if _, err := uuid.Parse(input.FaceMediaID); err != nil {
		return domain.CreatedBodyPresentation{}, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}
	assets, err := s.assets.GetReadyAssets(ctx, userID, []string{input.BodyMediaID, input.FaceMediaID})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.CreatedBodyPresentation{}, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
		}
		return domain.CreatedBodyPresentation{}, err
	}
	byID := make(map[string]domain.MediaAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	bodyAsset, hasBody := byID[input.BodyMediaID]
	faceAsset, hasFace := byID[input.FaceMediaID]
	if !hasBody || !hasFace || bodyAsset.Purpose != "body" || faceAsset.Purpose != "face" {
		return domain.CreatedBodyPresentation{}, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}

	created, err := s.repo.CreateBodyPresentation(ctx, userID, input, s.maxAttempts)
	if err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	if !created.Reused && s.billing != nil {
		if _, err := s.billing.Reserve(ctx, userID, created.Operation.ID, domain.ProductBodyOrbit, 1); err != nil {
			return domain.CreatedBodyPresentation{}, err
		}
	}
	if err := s.project(ctx, userID, &created.Presentation); err != nil {
		return domain.CreatedBodyPresentation{}, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (domain.BodyPresentation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return domain.BodyPresentation{}, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}
	item, err := s.repo.GetBodyPresentation(ctx, userID, id)
	if err != nil {
		return domain.BodyPresentation{}, err
	}
	if err := s.project(ctx, userID, &item); err != nil {
		return domain.BodyPresentation{}, err
	}
	return item.Presentation, nil
}

// Status 是实验室启动读模型：能力可用性 + 进行中/最新成功/最新失败。
func (s *Service) Status(ctx context.Context, userID string) (domain.BodyPresentationStatus, error) {
	active, completed, failed, err := s.repo.ListBodyPresentationStatus(ctx, userID)
	if err != nil {
		return domain.BodyPresentationStatus{}, err
	}
	out := domain.BodyPresentationStatus{Available: s.Available()}
	if active != nil {
		if err := s.project(ctx, userID, active); err != nil {
			return domain.BodyPresentationStatus{}, err
		}
		item := active.Presentation
		out.Active = &item
	}
	if completed != nil {
		if err := s.project(ctx, userID, completed); err != nil {
			return domain.BodyPresentationStatus{}, err
		}
		item := completed.Presentation
		out.Completed = &item
	}
	if failed != nil {
		if err := s.project(ctx, userID, failed); err != nil {
			return domain.BodyPresentationStatus{}, err
		}
		item := failed.Presentation
		out.Failed = &item
	}
	return out, nil
}

// project 签名视频/抽帧 URL 并投影任务状态。无任务（历史数据）时按结果推断。
func (s *Service) project(ctx context.Context, userID string, item *domain.StoredBodyPresentation) error {
	if s.signer != nil {
		if item.VideoStorageKey != "" {
			url, err := s.signer.Sign(ctx, item.VideoStorageKey)
			if err != nil {
				return err
			}
			item.Presentation.Orbit.VideoURL = url
		}
		for i := range item.Presentation.Orbit.Frames {
			if i >= len(item.FrameKeys) || item.FrameKeys[i] == "" {
				continue
			}
			url, err := s.signer.Sign(ctx, item.FrameKeys[i])
			if err != nil {
				return err
			}
			item.Presentation.Orbit.Frames[i].URL = url
		}
	}
	task, err := s.repo.BodyOrbitTaskState(ctx, userID, item.Presentation.ID)
	if err != nil {
		// 没有关联任务（异常数据）：有视频视为 completed，否则保持零值。
		if item.Presentation.Orbit.VideoURL != "" || item.VideoStorageKey != "" {
			item.Presentation.Status = "completed"
			item.Presentation.Progress = 100
		}
		return nil
	}
	projectTaskState(&item.Presentation, task)
	return nil
}

// projectTaskState 把任务状态映射到契约枚举 queued/processing/completed/failed。
func projectTaskState(item *domain.BodyPresentation, task domain.Task) {
	switch task.Status {
	case domain.TaskQueued:
		item.Status = "queued"
	case domain.TaskLeased, domain.TaskRetryWait:
		item.Status = "processing"
	case domain.TaskSucceeded:
		item.Status = "completed"
	default:
		item.Status = "failed"
	}
	item.Progress = task.ProgressBPS / 100
	item.Stage = task.StageCode
	if item.Status == "failed" {
		item.ErrorMessage = task.ErrorCode
	}
}
