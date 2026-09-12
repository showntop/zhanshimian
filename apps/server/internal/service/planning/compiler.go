package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

// RenderSpecSchemaVersion pins the canonical render_spec.v1 contract.
const RenderSpecSchemaVersion = "render_spec.v1"

// CompileRenderSpecs deterministically maps a gate-passed plan set onto one
// render spec per variant, ordered by slot. It reads only the typed step
// details — never page copy — and calls no AI.
func CompileRenderSpecs(planSet domain.PlanSet, report ReportSnapshot) ([]domain.RenderSpec, error) {
	variants := append([]domain.PlanVariant(nil), planSet.Variants...)
	sort.Slice(variants, func(i, j int) bool { return variants[i].Slot < variants[j].Slot })
	if len(variants) != 3 {
		return nil, fmt.Errorf("%w: plan set has %d variants", ErrRenderSpecInvalid, len(variants))
	}
	specs := make([]domain.RenderSpec, 0, len(variants))
	for _, variant := range variants {
		steps := map[domain.StepCategory]domain.PlanStep{}
		for _, step := range variant.Steps {
			steps[step.Category] = step
		}
		hair, ok := steps[domain.CategoryHair]
		if !ok {
			return nil, fmt.Errorf("%w: variant %s misses hair step", ErrRenderSpecInvalid, variant.Key)
		}
		makeup, ok := steps[domain.CategoryMakeup]
		if !ok {
			return nil, fmt.Errorf("%w: variant %s misses makeup step", ErrRenderSpecInvalid, variant.Key)
		}
		outfit, ok := steps[domain.CategoryOutfit]
		if !ok {
			return nil, fmt.Errorf("%w: variant %s misses outfit step", ErrRenderSpecInvalid, variant.Key)
		}
		directive := domain.RenderDirective{
			Identity: domain.RenderIdentity{
				BodyAssetID:            report.BodyAssetID,
				FaceAssetID:            report.FaceAssetID,
				PreserveIdentity:       true,
				PreserveBodyProportion: true,
				PreserveSkinTone:       true,
				PreserveAgeImpression:  true,
			},
			Composition: domain.RenderComposition{
				PreservePose:       true,
				PreserveBackground: true,
				PreserveLighting:   true,
				PreserveSourceCrop: true,
				AllowOutpaint:      false,
			},
			Hair: domain.RenderHair{
				Action: string(hair.Action), Target: hair.Details.Target, Intensity: hair.Details.Intensity,
			},
			Makeup: domain.RenderMakeup{
				Action: string(makeup.Action), Target: makeup.Details.Target, Intensity: makeup.Details.Intensity,
			},
			Outfit: domain.RenderOutfit{
				Silhouette: outfit.Details.Silhouette,
				Palette:    outfit.Details.Palette,
				Layers:     outfit.Details.Layers,
				Avoid:      outfit.Details.Avoid,
			},
			Output: domain.RenderOutput{
				MIMEType:     "image/jpeg",
				AspectPolicy: "preserve_body_source",
				Quality:      "high",
			},
		}
		if err := directive.Validate(); err != nil {
			return nil, fmt.Errorf("variant %s: %w", variant.Key, err)
		}
		specs = append(specs, domain.RenderSpec{
			ID:               uuid.NewString(),
			UserID:           planSet.UserID,
			PlanVariantID:    variant.ID,
			SourcePhotoSetID: report.PhotoSetID,
			SchemaVersion:    RenderSpecSchemaVersion,
			Spec:             directive,
			ContentHash:      renderSpecContentHash(directive),
		})
	}
	return specs, nil
}

// ValidateRenderSpec checks a stored spec against the render_spec.v1 rules.
func ValidateRenderSpec(spec domain.RenderSpec) error {
	return spec.Validate()
}

// ErrRenderSpecInvalid marks a render spec that violates render_spec.v1.
var ErrRenderSpecInvalid = domain.ErrRenderSpecInvalid

// renderSpecContentHash hashes the canonical directive JSON — the fields that
// define rendering behaviour — never database identity columns.
func renderSpecContentHash(directive domain.RenderDirective) string {
	encoded, err := json.Marshal(directive)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
