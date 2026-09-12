package media

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
)

type Service struct {
	repo     Repository
	objects  ObjectStore
	maxBytes int64
	grantTTL time.Duration
}

type CreatedIntent struct {
	domain.UploadIntent
	Grant domain.UploadGrant
}

func New(repo Repository, objects ObjectStore, maxBytes int64, grantTTL time.Duration) *Service {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if grantTTL <= 0 {
		grantTTL = 15 * time.Minute
	}
	return &Service{repo: repo, objects: objects, maxBytes: maxBytes, grantTTL: grantTTL}
}

func (s *Service) CreateUploadIntent(ctx context.Context, userID string, in CreateIntentInput) (CreatedIntent, error) {
	if err := validateCreate(in, s.maxBytes); err != nil {
		return CreatedIntent{}, err
	}
	intentID := uuid.NewString()
	intent, err := s.repo.CreateUploadIntent(ctx, domain.CreateUploadIntent{
		ID:        intentID,
		UserID:    userID,
		Purpose:   in.Purpose,
		MIMEType:  in.MIMEType,
		ByteSize:  in.ByteSize,
		SHA256:    in.SHA256,
		ObjectKey: fmt.Sprintf("users/%s/uploads/%s", userID, intentID),
		ExpiresAt: time.Now().Add(s.grantTTL),
	})
	if err != nil {
		return CreatedIntent{}, err
	}
	grant, err := s.objects.PresignUpload(ctx, intent, s.grantTTL)
	if err != nil {
		return CreatedIntent{}, err
	}
	return CreatedIntent{UploadIntent: intent, Grant: grant}, nil
}

func (s *Service) CompleteUploadIntent(ctx context.Context, userID, intentID string) (domain.MediaAsset, error) {
	asset, _, err := s.complete(ctx, userID, intentID)
	return asset, err
}

func (s *Service) CompleteVerifiedUpload(ctx context.Context, userID, intentID string) (domain.MediaAsset, bool, error) {
	return s.complete(ctx, userID, intentID)
}

func (s *Service) complete(ctx context.Context, userID, intentID string) (domain.MediaAsset, bool, error) {
	intent, err := s.repo.GetUploadIntent(ctx, userID, intentID)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	meta, err := s.objects.HeadObject(ctx, intent.ObjectKey)
	if err != nil {
		return domain.MediaAsset{}, false, fmt.Errorf("head upload object: %w", err)
	}
	if err := validateObject(intent, meta); err != nil {
		return domain.MediaAsset{}, false, err
	}
	return s.repo.CompleteUploadIntent(ctx, domain.CompleteUploadIntent{
		UserID: userID, IntentID: intentID, Metadata: meta,
	})
}
