package planning

import (
	"errors"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestCompileRenderSpecsPreservesIdentityCompositionAndBodyAspect(t *testing.T) {
	planSet := validDomainPlanSet()
	report := validReport(planSet.ReportID)
	specs, err := CompileRenderSpecs(planSet, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs", len(specs))
	}
	for _, item := range specs {
		spec := item.Spec
		if spec.Identity.FaceAssetID != report.FaceAssetID || spec.Identity.BodyAssetID != report.BodyAssetID {
			t.Fatalf("identity source mismatch: %#v", spec.Identity)
		}
		if !spec.Identity.PreserveIdentity || !spec.Identity.PreserveBodyProportion || !spec.Identity.PreserveSkinTone || !spec.Identity.PreserveAgeImpression {
			t.Fatal("identity preservation must be fail-closed")
		}
		if !spec.Composition.PreservePose || !spec.Composition.PreserveBackground || !spec.Composition.PreserveLighting || !spec.Composition.PreserveSourceCrop || spec.Composition.AllowOutpaint {
			t.Fatalf("unsafe composition: %#v", spec.Composition)
		}
		if spec.Output.MIMEType != "image/jpeg" || spec.Output.AspectPolicy != "preserve_body_source" || spec.Output.Quality != "high" {
			t.Fatalf("unsafe output: %#v", spec.Output)
		}
		if item.SourcePhotoSetID != report.PhotoSetID || item.PlanVariantID == "" {
			t.Fatalf("spec linkage incomplete: %#v", item)
		}
		if len(item.ContentHash) != 64 || strings.ContainsAny(item.ContentHash, "ABCDEF") {
			t.Fatalf("content hash not lowercase sha256: %q", item.ContentHash)
		}
		if err := ValidateRenderSpec(item); err != nil {
			t.Fatalf("compiled spec must validate: %v", err)
		}
	}
	if specs[0].PlanVariantID != planSet.Variants[0].ID || specs[2].PlanVariantID != planSet.Variants[2].ID {
		t.Fatal("specs must be ordered by variant slot")
	}
}

func TestCompileRenderSpecsDoesNotInventKeepAdjustments(t *testing.T) {
	planSet := validDomainPlanSet()
	planSet.Variants[0].Steps[0].Action = domain.ActionKeep
	planSet.Variants[0].Steps[0].Details.Target = "保持原发型，只整理碎发"
	specs, err := CompileRenderSpecs(planSet, validReport(planSet.ReportID))
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Spec.Hair.Action != "keep" || specs[0].Spec.Hair.Target != "保持原发型，只整理碎发" {
		t.Fatalf("keep was changed: %#v", specs[0].Spec.Hair)
	}
}

func TestValidateRenderSpecRejectsUnsafeDirectives(t *testing.T) {
	specs, err := CompileRenderSpecs(validDomainPlanSet(), validReport("20000000-0000-0000-0000-000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	spec := specs[0]
	spec.SchemaVersion = "render_spec.v0"
	if err := ValidateRenderSpec(spec); !errors.Is(err, ErrRenderSpecInvalid) {
		t.Fatalf("version check: %v", err)
	}
	spec = specs[0]
	spec.Spec.Composition.AllowOutpaint = true
	if err := ValidateRenderSpec(spec); !errors.Is(err, ErrRenderSpecInvalid) {
		t.Fatalf("outpaint check: %v", err)
	}
	spec = specs[0]
	spec.Spec.Hair.Intensity = "high"
	if err := ValidateRenderSpec(spec); !errors.Is(err, ErrRenderSpecInvalid) {
		t.Fatalf("intensity check: %v", err)
	}
	spec = specs[0]
	spec.Spec.Output.MIMEType = "image/webp"
	if err := ValidateRenderSpec(spec); !errors.Is(err, ErrRenderSpecInvalid) {
		t.Fatalf("mime check: %v", err)
	}
}

func TestCompileRenderSpecsRejectsIncompleteVariants(t *testing.T) {
	planSet := validDomainPlanSet()
	planSet.Variants[1].Steps = planSet.Variants[1].Steps[:2]
	if _, err := CompileRenderSpecs(planSet, validReport(planSet.ReportID)); !errors.Is(err, ErrRenderSpecInvalid) {
		t.Fatalf("got %v, want ErrRenderSpecInvalid", err)
	}
}

// validDomainPlanSet converts the gate-valid candidate into the published
// domain graph shape with stable row IDs.
func validDomainPlanSet() domain.PlanSet {
	candidate := validValidationInput().Candidate
	set := domain.PlanSet{
		ID:                   "10000000-0000-0000-0000-000000000001",
		UserID:               "00000000-0000-0000-0000-000000000001",
		ReportID:             "20000000-0000-0000-0000-000000000001",
		ProfileSnapshot:      []byte(`{"role":"designer"}`),
		Scene:                domain.SceneDaily,
		SceneBrief:           validBrief(),
		PlannerSchemaVersion: PlannerSchemaVersion,
		StyleRuleVersion:     StyleRuleVersion,
	}
	set.BriefHash = BriefHash(set.SceneBrief)
	for _, variant := range candidate.Variants {
		domainVariant := domain.PlanVariant{
			ID:             variantKeyToID(variant.Key),
			UserID:         set.UserID,
			PlanSetID:      set.ID,
			Slot:           variant.Slot,
			Key:            variant.Key,
			Name:           variant.Name,
			Descriptor:     variant.Descriptor,
			Rationale:      variant.Rationale,
			Recommended:    variant.Recommended,
			OutcomeTags:    variant.OutcomeTags,
			DifferenceTags: variant.DifferenceTags,
		}
		for _, step := range variant.Steps {
			domainStep := domain.PlanStep{
				ID:            variantKeyToID(variant.Key) + "-step-" + string(step.Category),
				UserID:        set.UserID,
				PlanVariantID: domainVariant.ID,
				Category:      step.Category,
				Action:        step.Action,
				Title:         step.Title,
				Summary:       step.Summary,
				Details:       step.Details,
				Position:      stepPosition(step.Category),
			}
			for _, grounding := range step.Groundings {
				domainStep.Groundings = append(domainStep.Groundings, domain.PlanStepGrounding{
					UserID: set.UserID, PlanStepID: domainStep.ID,
					SourceType: grounding.SourceType, SourceID: grounding.SourceID, Reason: grounding.Reason,
				})
			}
			domainVariant.Steps = append(domainVariant.Steps, domainStep)
		}
		set.Variants = append(set.Variants, domainVariant)
	}
	return set
}

func variantKeyToID(key domain.PlanVariantKey) string {
	switch key {
	case domain.VariantSharp:
		return "60000000-0000-0000-0000-000000000001"
	case domain.VariantWarm:
		return "60000000-0000-0000-0000-000000000002"
	default:
		return "60000000-0000-0000-0000-000000000003"
	}
}

func stepPosition(category domain.StepCategory) int {
	switch category {
	case domain.CategoryHair:
		return 1
	case domain.CategoryMakeup:
		return 2
	default:
		return 3
	}
}
