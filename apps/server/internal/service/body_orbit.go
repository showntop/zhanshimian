package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func (s *Service) CreateBodyPresentation(ctx context.Context, userID string, input domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, error) {
	if s.orbitGenerator == nil {
		return domain.BodyPresentation{}, nil, fmt.Errorf("%w: 3D 形象暂未开放", ErrCapabilityUnavailable)
	}
	if _, err := uuid.Parse(input.BodyMediaID); err != nil {
		return domain.BodyPresentation{}, nil, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}
	if _, err := uuid.Parse(input.FaceMediaID); err != nil {
		return domain.BodyPresentation{}, nil, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}
	assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{input.BodyMediaID, input.FaceMediaID})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.BodyPresentation{}, nil, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
		}
		return domain.BodyPresentation{}, nil, err
	}
	byID := make(map[string]domain.MediaAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	body, hasBody := byID[input.BodyMediaID]
	face, hasFace := byID[input.FaceMediaID]
	if !hasBody || !hasFace || body.Kind != "body" || face.Kind != "face" {
		return domain.BodyPresentation{}, nil, fmt.Errorf("%w: 需要正脸和正面全身", ErrValidation)
	}

	var chargeRefs []string
	if !s.hasActiveTaskType(ctx, userID, string(domain.TaskTypeBodyOrbit)) {
		refs, authErr := s.authorize(ctx, userID, domainActionLook, "", 1)
		if authErr != nil {
			return domain.BodyPresentation{}, nil, authErr
		}
		chargeRefs = refs
	}
	item, task, err := s.repo.CreateBodyPresentation(ctx, userID, input)
	if err != nil {
		s.refundRefs(ctx, chargeRefs)
		return domain.BodyPresentation{}, nil, err
	}
	if task != nil && len(chargeRefs) > 0 {
		s.bindCharges(ctx, chargeRefs, []string{task.ID})
	} else {
		s.refundRefs(ctx, chargeRefs)
	}
	s.attachBodyPresentation(ctx, userID, &item)
	return item, task, nil
}

func (s *Service) GetBodyPresentation(ctx context.Context, userID, id string) (domain.BodyPresentation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return domain.BodyPresentation{}, repository.ErrNotFound
	}
	item, err := s.repo.GetBodyPresentation(ctx, userID, id)
	if err != nil {
		return domain.BodyPresentation{}, err
	}
	s.attachBodyPresentation(ctx, userID, &item)
	return item, nil
}

func (s *Service) GetBodyPresentationStatus(ctx context.Context, userID string) (domain.BodyPresentationStatus, error) {
	active, completed, failed, err := s.repo.ListBodyPresentationStatus(ctx, userID)
	if err != nil {
		return domain.BodyPresentationStatus{}, err
	}
	if active != nil {
		s.attachBodyPresentation(ctx, userID, active)
	}
	if completed != nil {
		s.attachBodyPresentation(ctx, userID, completed)
	}
	if failed != nil {
		s.attachBodyPresentation(ctx, userID, failed)
	}
	return domain.BodyPresentationStatus{
		Available: s.orbitGenerator != nil,
		Active:    active,
		Completed: completed,
		Failed:    failed,
	}, nil
}

func (s *Service) attachBodyPresentation(ctx context.Context, userID string, item *domain.BodyPresentation) {
	if item == nil {
		return
	}
	if item.Orbit.VideoURL != "" {
		item.Orbit.VideoURL = s.absoluteURL(item.Orbit.VideoURL)
	}
	for i := range item.Orbit.Frames {
		if item.Orbit.Frames[i].URL != "" {
			item.Orbit.Frames[i].URL = s.absoluteURL(item.Orbit.Frames[i].URL)
		}
	}
	tasks, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypeBodyOrbit, "presentation_id", []string{item.ID})
	if err != nil {
		return
	}
	if task, ok := tasks[item.ID]; ok {
		view := decodeTaskErrorView(taskView(task))
		item.Task = ptrTaskView(view)
		item.Status = task.Status
		item.Progress = task.Progress
		item.Stage = task.Stage
		if task.Status == domain.TaskFailed && view.Error != nil {
			item.ErrorMessage = view.Error.Message
		}
	}
}
