package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	// MIME 以服务端嗅探为准：客户端声明是按扩展名猜的（PNG 临时文件常常
	// 没有 .png 后缀），猜错的声明会让下游技术校验误判「照片格式无法确认」。
	// 不是 JPEG/PNG 的字节在上传完成时就拒掉，而不是拖到分析阶段才报。
	detected, err := s.sniffMIME(ctx, intent.ObjectKey)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	if detected != "image/jpeg" && detected != "image/png" {
		return domain.MediaAsset{}, false, fmt.Errorf("%w: 仅支持 JPEG 或 PNG，请更换照片后重试", ErrValidation)
	}
	meta.MIMEType = detected
	return s.repo.CompleteUploadIntent(ctx, domain.CompleteUploadIntent{
		UserID: userID, IntentID: intentID, Metadata: meta,
	})
}

func (s *Service) sniffMIME(ctx context.Context, objectKey string) (string, error) {
	rc, err := s.objects.Open(ctx, objectKey)
	if err != nil {
		return "", fmt.Errorf("open upload object: %w", err)
	}
	defer func() { _ = rc.Close() }()
	head := make([]byte, 512)
	n, err := io.ReadAtLeast(rc, head, 1)
	if err != nil {
		return "", fmt.Errorf("read upload object: %w", err)
	}
	return http.DetectContentType(head[:n]), nil
}
