package billing

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

var (
	ErrInsufficientCredits = domain.ErrInsufficientCredits
	ErrAlreadySettled      = domain.ErrAlreadySettled
	ErrInvalidRefundReason = domain.ErrInvalidRefundReason
)

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Reserve(ctx context.Context, in domain.ReserveBilling) (domain.BillingReservation, bool, error) {
	return s.repo.Reserve(ctx, in)
}

func (s *Service) Settle(ctx context.Context, in domain.SettleBilling) (domain.BillingReservation, bool, error) {
	return s.repo.Settle(ctx, in)
}

func (s *Service) Refund(ctx context.Context, in domain.RefundBilling) (domain.BillingReservation, bool, error) {
	return s.repo.Refund(ctx, in)
}
