package feedback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

var (
	ctx              = context.Background()
	userID           = "11111111-1111-1111-1111-111111111111"
	publicationID    = "22222222-2222-2222-2222-222222222222"
	renderRunID      = "33333333-3333-3333-3333-333333333333"
	candidateID      = "44444444-4444-4444-4444-444444444444"
	publishedAssetID = "55555555-5555-5555-5555-555555555555"
	foreignAssetID   = "66666666-6666-6666-6666-666666666666"
)

func ptr(s string) *string { return &s }

func TestCreateGenerationFeedbackDerivesPublicationChain(t *testing.T) {
	repo := newFeedbackFake()
	got, created, err := New(repo).CreateGenerationFeedback(ctx, userID, CreateGenerationFeedbackInput{
		PublicationID:  publicationID,
		Tags:           []domain.Tag{domain.GenerationHairMismatch},
		IdempotencyKey: "generation-feedback-1",
	})
	if err != nil || !created {
		t.Fatalf("%#v %v", got, err)
	}
	if got.RenderRunID != renderRunID || got.CandidateID != candidateID ||
		got.AssetID != publishedAssetID || got.Generation != 2 {
		t.Fatalf("publication chain was not frozen: %#v", got)
	}
	if got.MediaAssetID != nil || got.AcknowledgementCode != domain.AckFeedbackRecorded {
		t.Fatalf("optional photo or acknowledgement wrong: %#v", got)
	}
}

func TestCreateGenerationFeedbackRejectsForeignFeedbackPhoto(t *testing.T) {
	repo := newFeedbackFake()
	_, _, err := New(repo).CreateGenerationFeedback(ctx, userID, CreateGenerationFeedbackInput{
		PublicationID:  publicationID,
		Tags:           []domain.Tag{domain.GenerationUnnatural},
		MediaAssetID:   ptr(foreignAssetID),
		IdempotencyKey: "generation-feedback-2",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

// fakeRepository 用内存结构模拟派生发布链路、反馈图片归属与幂等重放。
type fakeRepository struct {
	renderRunID      string
	candidateID      string
	publishedAssetID string
	generation       int

	ownedMedia map[string]bool

	byKey     map[string]domain.GenerationFeedback
	hashByKey map[string]string
}

func newFeedbackFake() *fakeRepository {
	return &fakeRepository{
		renderRunID:      renderRunID,
		candidateID:      candidateID,
		publishedAssetID: publishedAssetID,
		generation:       2,
		ownedMedia:       map[string]bool{},
		byKey:            map[string]domain.GenerationFeedback{},
		hashByKey:        map[string]string{},
	}
}

func (f *fakeRepository) CreateGenerationFeedback(_ context.Context, command CreateGenerationFeedbackCommand) (domain.GenerationFeedback, bool, error) {
	if existing, ok := f.byKey[command.IdempotencyKey]; ok {
		if f.hashByKey[command.IdempotencyKey] != command.RequestHash {
			return domain.GenerationFeedback{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	if command.MediaAssetID != nil && !f.ownedMedia[*command.MediaAssetID] {
		return domain.GenerationFeedback{}, false, repository.ErrNotFound
	}
	fb := domain.GenerationFeedback{
		ID:            uuid.NewString(),
		PublicationID: command.PublicationID,
		RenderRunID:   f.renderRunID,
		CandidateID:   f.candidateID,
		AssetID:       f.publishedAssetID,
		Generation:    f.generation,
		Tags:          command.Tags,
		Comment:       command.Comment,
		MediaAssetID:  command.MediaAssetID,
		CreatedAt:     time.Now(),
	}
	f.byKey[command.IdempotencyKey] = fb
	f.hashByKey[command.IdempotencyKey] = command.RequestHash
	return fb, true, nil
}
