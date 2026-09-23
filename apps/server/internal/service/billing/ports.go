package billing

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Lifecycle is the full operation-lifecycle billing surface: reservations are
// created up front, then settled or refunded as the operation reaches a
// terminal state. The postgres adapter satisfies it directly.
type Lifecycle interface {
	Reserve(ctx context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error)
	Settle(ctx context.Context, userID, operationID string, publicationID *string) error
	Refund(ctx context.Context, userID, operationID string) error
	ReconcileTerminalOperations(ctx context.Context, limit int) (domain.ReconcileResult, error)
}

// Reserver is the narrow reserve-only view the assessment, planning and
// rendering services need to charge an operation at creation time.
type Reserver interface {
	Reserve(ctx context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error)
}
