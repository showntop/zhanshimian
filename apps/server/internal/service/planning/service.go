package planning

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/billing"
)

// Dependencies carries the narrow ports a Planning service needs. The
// operation/task retry budgets live in the adapters (which read them from the
// registry definition), never in the commands.
type Dependencies struct {
	Reports    ReportReader
	Operations OperationStarter
	Store      PlanSetStore
	Renders    CurrentRenderReader
	Memories   PreferenceMemoryReader
	Billing    billing.Reserver
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
	report, err := s.deps.Reports.GetPlanningReport(ctx, cmd.UserID, cmd.ReportID)
	if err != nil {
		return CreateResult{}, err
	}
	memories, err := s.listMemories(ctx, cmd.UserID)
	if err != nil {
		return CreateResult{}, err
	}
	briefHash := BriefHash(brief)
	key := PlanSetKey{
		UserID:               cmd.UserID,
		ReportID:             cmd.ReportID,
		Scene:                brief.Scene,
		BriefHash:            briefHash,
		PlanningInputHash:    PlanningInputHash(report.ID, report.ProfileSnapshot, briefHash, memories),
		PlannerSchemaVersion: PlannerSchemaVersion,
	}
	if cmd.Refresh {
		// 强制重生成（不满意重出）：跳过已发布复用，并给本次尝试派生一次性身份——
		// 方案集 id / operation / task 全部由带 nonce 的键自然派生。
		key.PlanningInputHash = RegenerationInputHash(key.PlanningInputHash, s.newID())
	} else if published, found, err := s.deps.Store.FindPublished(ctx, key); err != nil {
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
				PlanningInputHash:    key.PlanningInputHash,
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
	if created && s.deps.Billing != nil {
		if _, err := s.deps.Billing.Reserve(ctx, cmd.UserID, ref.ID, domain.ProductPlanSet, 1); err != nil {
			return CreateResult{}, err
		}
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
	if err := s.mergeRenders(ctx, userID, &planSet); err != nil {
		return domain.PlanSet{}, err
	}
	return planSet, nil
}

// ListPlanSets 返回报告下的方案集列表;渲染状态跨方案集一次批量合并,
// 不按方案集逐个查询。
func (s *Service) ListPlanSets(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error) {
	sets, err := s.deps.Store.List(ctx, userID, reportID, scene)
	if err != nil {
		return nil, err
	}
	if s.deps.Renders == nil || len(sets) == 0 {
		return sets, nil
	}
	variantIDs := make([]string, 0, len(sets)*3)
	for _, planSet := range sets {
		for _, variant := range planSet.Variants {
			variantIDs = append(variantIDs, variant.ID)
		}
	}
	current, err := s.deps.Renders.ListCurrentByVariantIDs(ctx, userID, variantIDs)
	if err != nil {
		return nil, err
	}
	for index := range sets {
		mergeRenderViews(&sets[index], current)
	}
	return sets, nil
}

// mergeRenders 为单套方案集批量读取并合并各 variant 的当前渲染视图。
func (s *Service) mergeRenders(ctx context.Context, userID string, planSet *domain.PlanSet) error {
	if s.deps.Renders == nil {
		return nil
	}
	variantIDs := make([]string, 0, len(planSet.Variants))
	for _, variant := range planSet.Variants {
		variantIDs = append(variantIDs, variant.ID)
	}
	current, err := s.deps.Renders.ListCurrentByVariantIDs(ctx, userID, variantIDs)
	if err != nil {
		return err
	}
	mergeRenderViews(planSet, current)
	return nil
}

// mergeRenderViews 把渲染读模型的当前视图挂到各 variant 上(整份视图透传,
// state/operation/retryable/media/publication 都取自渲染侧事实),并聚合整体状态。
func mergeRenderViews(planSet *domain.PlanSet, current map[string]domain.RenderRunView) {
	for index := range planSet.Variants {
		if view, ok := current[planSet.Variants[index].ID]; ok {
			status := view.Render
			planSet.Variants[index].Render = &status
		}
	}
	planSet.RenderState = planSetRenderState(planSet.Variants)
}

// planSetRenderState:全部 ready→ready;至少一个 ready 且其余失败/生成中→
// ready_partial;全部终态失败→failed;其余(含全部尚未触发渲染)→rendering。
func planSetRenderState(variants []domain.PlanVariant) string {
	ready, failed, total := 0, 0, len(variants)
	for _, variant := range variants {
		if variant.Render == nil {
			continue
		}
		switch variant.Render.State {
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

func (s *Service) newID() string {
	if s.deps.IDs != nil {
		return s.deps.IDs()
	}
	return uuid.NewString()
}

// listMemories reads the user's recent preference memories, tolerating a
// nil reader so callers that never wired memories still behave correctly.
func (s *Service) listMemories(ctx context.Context, userID string) ([]domain.PreferenceMemory, error) {
	if s.deps.Memories == nil {
		return nil, nil
	}
	return s.deps.Memories.ListPreferenceMemories(ctx, userID, planningMemoryLimit)
}

// generationDedupeKey is the semantic dedupe key shared by the operation and
// every content task of one plan set identity. It keys on the planning input
// hash so a new preference memory yields a new identity rather than reusing
// stale published content.
func generationDedupeKey(key PlanSetKey) string {
	return fmt.Sprintf("plan-set:%s:%s:%s",
		key.ReportID, key.PlanningInputHash, key.PlannerSchemaVersion)
}

// semanticPlanSetID derives a stable UUID from the semantic key so duplicate
// starts converge on the same plan set identity.
func semanticPlanSetID(key PlanSetKey) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(generationDedupeKey(key))).String()
}
