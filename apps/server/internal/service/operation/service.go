package operation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
)

var ErrValidation = errors.New("validation error")

type Service struct {
	reader Reader
}

func New(reader Reader) *Service {
	return &Service{reader: reader}
}

func (s *Service) Get(ctx context.Context, userID, id string) (domain.Operation, error) {
	return s.reader.GetOperation(ctx, userID, strings.TrimSpace(id))
}

func (s *Service) GetMany(ctx context.Context, userID string, ids []string) ([]domain.Operation, error) {
	cleaned, err := normalizeIDs(ids)
	if err != nil {
		return nil, err
	}
	return s.reader.GetOperations(ctx, userID, cleaned)
}

func normalizeIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 20 {
		return nil, fmt.Errorf("%w: query 1 to 20 operation ids", ErrValidation)
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("%w: operation id is not a uuid", ErrValidation)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("%w: duplicate operation id", ErrValidation)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}
