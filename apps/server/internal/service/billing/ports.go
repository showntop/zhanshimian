package billing

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

type Repository interface {
	Reserve(context.Context, domain.ReserveBilling) (domain.BillingReservation, bool, error)
	Settle(context.Context, domain.SettleBilling) (domain.BillingReservation, bool, error)
	Refund(context.Context, domain.RefundBilling) (domain.BillingReservation, bool, error)
}
