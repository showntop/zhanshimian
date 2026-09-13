package planning

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

// Dependencies carries the narrow ports a Planning service needs. The
// operation/task retry budgets live in the adapters (which read them from the
// registry definition), never in the commands.
type Dependencies struct {
	Reports    ReportReader
	Operations OperationStarter
	Store      PlanSetStore
	Renders    CurrentRenderReader
	IDs        func() string
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

// CreatePlanSet normalizes the brief, reuses any published result for the
// same semantic key, and otherwise atomically starts the operation and its
// first content task. The plan set ID is derived from the semantic key so a
// duplicate start converges on the same identity.
func (s *Service) CreatePlanSet(ctx context.Context, cmd CreateCommand) (CreateResult, error) {
	brief, err := NormalizeBrief(cmd.Scene, cmd.Answers)
	if err != nil {
		return CreateResult{}, err
	}
	// The read doubles as the tenant check: a foreign report reads as
	// not-found before anything is created.
	if _, err := s.deps.Reports.GetPlanningReport(ctx, cmd.UserID, cmd.ReportID); err != nil {
		return CreateResult{}, err
	}
	key := PlanSetKey{
		UserID:               cmd.UserID,
		ReportID:             cmd.ReportID,
		Scene:                brief.Scene,
		BriefHash:            BriefHash(brief),
		PlannerSchemaVersion: PlannerSchemaVersion,
	}
	if published, found, err := s.deps.Store.FindPublished(ctx, key); err != nil {
		return CreateResult{}, err
	} else if found {
		return CreateResult{PlanSetID: published.ID, Accepted: false, PlanSet: &published}, nil
	}
	planSetID := semanticPlanSetID(key)
	ref, created, err := s.deps.Operations.StartWithTask(ctx, StartOperationCommand{
		OperationID:    s.newID(),
		UserID:         cmd.UserID,
		Kind:           domain.OperationPlanSet,
		SubjectType:    "plan_set",
		SubjectID:      planSetID,
		IdempotencyKey: cmd.IdempotencyKey,
		DedupeKey:      generationDedupeKey(key),
		Task: EnqueueTask{
			Type:              PlanSetGenerationTaskType,
			SubjectType:       "plan_set",
			SubjectID:         planSetID,
			SubjectGeneration: 1,
			PayloadVersion:    1,
			Payload: GenerateTaskPayload{
				PlanSetID:            planSetID,
				ReportID:             cmd.ReportID,
				Scene:                brief.Scene,
				Brief:                brief,
				BriefHash:            key.BriefHash,
				PlannerSchemaVersion: PlannerSchemaVersion,
				StyleRuleVersion:     StyleRuleVersion,
				ContentAttempt:       1,
				PriorReasonCodes:     nil,
			},
			DedupeKey: generationDedupeKey(key),
		},
	})
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{PlanSetID: planSetID, Accepted: created, Operation: ref}, nil
}

// GetPlanSet 返回不可变方案图;若装配了渲染只读端口,则在一次批量读取中
// 合并各 variant 的当前渲染状态(不逐套 N+1,不写渲染表)。
func (s *Service) GetPlanSet(ctx context.Context, userID, planSetID string) (domain.PlanSet, error) {
	planSet, err := s.deps.Store.Get(ctx, userID, planSetID)
	if err != nil {
		return domain.PlanSet{}, err
	}
	if s.deps.Renders == nil {
		return planSet, nil
	}
	variantIDs := make([]string, 0, len(planSet.Variants))
	for _, variant := range planSet.Variants {
		variantIDs = append(variantIDs, variant.ID)
	}
	current, err := s.deps.Renders.ListCurrentByVariantIDs(ctx, userID, variantIDs)
	if err != nil {
		return domain.PlanSet{}, err
	}
	for index := range planSet.Variants {
		if view, ok := current[planSet.Variants[index].ID]; ok {
			planSet.Variants[index].RenderState = view.Render.State
			planSet.Variants[index].RenderOperationID = view.Render.OperationID
			planSet.Variants[index].HasRenderMedia = view.Render.Media != nil
		}
	}
	planSet.RenderState = planSetRenderState(planSet.Variants)
	return planSet, nil
}

// planSetRenderState:全部 ready→ready;至少一个 ready 且其余失败/生成中→
// ready_partial;全部终态失败→failed;其余→rendering。
func planSetRenderState(variants []domain.PlanVariant) string {
	ready, failed, total := 0, 0, len(variants)
	for _, variant := range variants {
		switch variant.RenderState {
		case domain.RenderStateReady:
			ready++
		case domain.RenderStateFailed, domain.RenderStateUnavailable:
			failed++
		}
	}
	switch {
	case ready == total:
		return "ready"
	case ready > 0:
		return "ready_partial"
	case failed == total && total > 0:
		return "failed"
	default:
		return "rendering"
	}
}

func (s *Service) ListPlanSets(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error) {
	return s.deps.Store.List(ctx, userID, reportID, scene)
}

func (s *Service) newID() string {
	if s.deps.IDs != nil {
		return s.deps.IDs()
	}
	return uuid.NewString()
}

// generationDedupeKey is the semantic dedupe key shared by the operation and
// every content task of one plan set identity.
func generationDedupeKey(key PlanSetKey) string {
	return fmt.Sprintf("plan-set:%s:%s:%s:%s",
		key.ReportID, key.Scene, key.BriefHash, key.PlannerSchemaVersion)
}

// semanticPlanSetID derives a stable UUID from the semantic key so duplicate
// starts converge on the same plan set identity.
func semanticPlanSetID(key PlanSetKey) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(generationDedupeKey(key))).String()
}
