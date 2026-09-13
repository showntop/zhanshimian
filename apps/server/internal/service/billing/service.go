package billing

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

var (
	ErrInsufficientCredits = domain.ErrInsufficientCredits
	ErrAlreadySettled      = domain.ErrAlreadySettled
	ErrAlreadyRefunded     = domain.ErrAlreadyRefunded
	ErrReservationConflict = domain.ErrReservationConflict
)

type Service struct {
	repo Lifecycle
}

func New(repo Lifecycle) *Service {
	return &Service{repo: repo}
}

func (s *Service) Reserve(ctx context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error) {
	return s.repo.Reserve(ctx, userID, operationID, product, units)
}

func (s *Service) Settle(ctx context.Context, userID, operationID string, publicationID *string) error {
	return s.repo.Settle(ctx, userID, operationID, publicationID)
}

func (s *Service) Refund(ctx context.Context, userID, operationID string) error {
	return s.repo.Refund(ctx, userID, operationID)
}

func (s *Service) ReconcileTerminalOperations(ctx context.Context, limit int) (domain.ReconcileResult, error) {
	return s.repo.ReconcileTerminalOperations(ctx, limit)
}
