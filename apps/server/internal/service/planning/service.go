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

func (s *Service) GetPlanSet(ctx context.Context, userID, planSetID string) (domain.PlanSet, error) {
	return s.deps.Store.Get(ctx, userID, planSetID)
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
