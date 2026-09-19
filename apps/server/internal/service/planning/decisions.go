package planning

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrInvalidDecision marks a decision value outside the closed like/skip
// domain, or a malformed variant id. The transport maps it to 400 — the
// request is wrong, not the world.
var ErrInvalidDecision = errors.New("invalid plan variant decision")

// errDecisionStoreUnavailable means the decision store was never wired: the
// transport maps unknown errors to 500, which is the honest answer for a
// misconfigured server (bootstrap always wires it in production).
var errDecisionStoreUnavailable = errors.New("decision store unavailable")

// PutVariantDecision validates the decision value and the variant id, then
// delegates the UPSERT to the store. Ownership is structural: the composite
// (user_id, plan_variant_id) foreign key means a foreign or unknown variant
// reads as not-found, and the plan set id is resolved server-side (the client
// never sends it). Decisions never touch billing.
func (s *Service) PutVariantDecision(ctx context.Context, userID, planVariantID, decision string) (domain.PlanVariantDecision, error) {
	kind := domain.PlanVariantDecisionKind(decision)
	if kind != domain.DecisionLike && kind != domain.DecisionSkip {
		return domain.PlanVariantDecision{}, ErrInvalidDecision
	}
	if _, err := uuid.Parse(planVariantID); err != nil {
		return domain.PlanVariantDecision{}, ErrInvalidDecision
	}
	if s.deps.Decisions == nil {
		return domain.PlanVariantDecision{}, errDecisionStoreUnavailable
	}
	return s.deps.Decisions.UpsertVariantDecision(ctx, userID, domain.UpsertVariantDecisionCommand{
		PlanVariantID: planVariantID,
		Decision:      kind,
	})
}

// DeleteVariantDecision clears a decision (undo). Deleting an absent row is
// success: undo means "back to undecided", it does not depend on the decision
// ever having been persisted.
func (s *Service) DeleteVariantDecision(ctx context.Context, userID, planVariantID string) error {
	if _, err := uuid.Parse(planVariantID); err != nil {
		return ErrInvalidDecision
	}
	if s.deps.Decisions == nil {
		return errDecisionStoreUnavailable
	}
	return s.deps.Decisions.DeleteVariantDecision(ctx, userID, planVariantID)
}

// mergeDecisions 为单套方案集批量读取并合并各 variant 的当前决策(读投影,
// 不参与渲染/文字/指纹之外的任何语义;nil store 容忍)。
func (s *Service) mergeDecisions(ctx context.Context, userID string, planSet *domain.PlanSet) error {
	if s.deps.Decisions == nil || len(planSet.Variants) == 0 {
		return nil
	}
	variantIDs := make([]string, 0, len(planSet.Variants))
	for _, variant := range planSet.Variants {
		variantIDs = append(variantIDs, variant.ID)
	}
	decisions, err := s.deps.Decisions.ListDecisionsByVariantIDs(ctx, userID, variantIDs)
	if err != nil {
		return err
	}
	mergeDecisionViews(planSet, decisions)
	return nil
}

// mergeDecisionViews 把决策读模型的当前视图挂到各 variant 上;查不到的保持
// nil(未决)。
func mergeDecisionViews(planSet *domain.PlanSet, decisions map[string]domain.PlanVariantDecision) {
	for index := range planSet.Variants {
		if decision, ok := decisions[planSet.Variants[index].ID]; ok {
			value := decision
			planSet.Variants[index].Decision = &value
		}
	}
}

// listDecisions reads the user's recent variant decisions for the planning
// fingerprint, tolerating a nil store so callers that never wired decisions
// still behave correctly.
func (s *Service) listDecisions(ctx context.Context, userID string) ([]domain.VariantDecisionItem, error) {
	if s.deps.Decisions == nil {
		return nil, nil
	}
	return s.deps.Decisions.ListRecentDecisions(ctx, userID, planningDecisionLimit)
}
