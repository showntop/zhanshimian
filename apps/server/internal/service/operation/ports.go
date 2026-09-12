package operation

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

type Reader interface {
	GetOperation(context.Context, string, string) (domain.Operation, error)
	GetOperations(context.Context, string, []string) ([]domain.Operation, error)
}
