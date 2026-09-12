package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

// RenderSpecSchemaVersion pins the canonical render_spec.v1 contract.
const RenderSpecSchemaVersion = "render_spec.v1"

// ErrRenderSpecInvalid marks a render spec that violates render_spec.v1.
var ErrRenderSpecInvalid = errors.New("render spec violates render_spec.v1")

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
		if err := ValidateRenderSpecDirective(directive); err != nil {
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
	if spec.SchemaVersion != RenderSpecSchemaVersion {
		return fmt.Errorf("%w: schema version %q", ErrRenderSpecInvalid, spec.SchemaVersion)
	}
	return ValidateRenderSpecDirective(spec.Spec)
}

// ValidateRenderSpecDirective enforces the fail-closed preservation and
// output rules on the directive itself.
func ValidateRenderSpecDirective(directive domain.RenderDirective) error {
	identity := directive.Identity
	if identity.BodyAssetID == "" || identity.FaceAssetID == "" {
		return fmt.Errorf("%w: identity assets missing", ErrRenderSpecInvalid)
	}
	if !identity.PreserveIdentity || !identity.PreserveBodyProportion || !identity.PreserveSkinTone || !identity.PreserveAgeImpression {
		return fmt.Errorf("%w: identity preservation must be fail-closed", ErrRenderSpecInvalid)
	}
	composition := directive.Composition
	if !composition.PreservePose || !composition.PreserveBackground || !composition.PreserveLighting || !composition.PreserveSourceCrop || composition.AllowOutpaint {
		return fmt.Errorf("%w: composition preservation must be fail-closed", ErrRenderSpecInvalid)
	}
	if err := validateAppearanceStep("hair", directive.Hair.Action, directive.Hair.Target, directive.Hair.Intensity); err != nil {
		return err
	}
	if err := validateAppearanceStep("makeup", directive.Makeup.Action, directive.Makeup.Target, directive.Makeup.Intensity); err != nil {
		return err
	}
	outfit := directive.Outfit
	if len(outfit.Palette) < 1 || len(outfit.Palette) > 8 {
		return fmt.Errorf("%w: outfit palette wants 1-8 colors, got %d", ErrRenderSpecInvalid, len(outfit.Palette))
	}
	if len(outfit.Layers) > 8 || len(outfit.Avoid) > 8 {
		return fmt.Errorf("%w: outfit layers/avoid exceed 8 items", ErrRenderSpecInvalid)
	}
	if outfit.Silhouette == "" {
		return fmt.Errorf("%w: outfit silhouette required", ErrRenderSpecInvalid)
	}
	output := directive.Output
	if output.MIMEType != "image/jpeg" || output.AspectPolicy != "preserve_body_source" || output.Quality != "high" {
		return fmt.Errorf("%w: unsafe output policy %#v", ErrRenderSpecInvalid, output)
	}
	return nil
}

func validateAppearanceStep(name, action, target, intensity string) error {
	if action != string(domain.ActionKeep) && action != string(domain.ActionAdjust) {
		return fmt.Errorf("%w: %s action %q", ErrRenderSpecInvalid, name, action)
	}
	if target == "" {
		return fmt.Errorf("%w: %s target required", ErrRenderSpecInvalid, name)
	}
	if intensity != "low" && intensity != "medium" {
		return fmt.Errorf("%w: %s intensity must be low|medium, got %q", ErrRenderSpecInvalid, name, intensity)
	}
	return nil
}

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
