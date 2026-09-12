package planning

import (
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestNormalizeBriefAcceptsEveryScene(t *testing.T) {
	tests := []struct {
		scene   domain.Scene
		answers map[string]string
	}{
		{domain.SceneGeneral, map[string]string{"focus": "balanced", "preparation": "closet", "impression": "natural"}},
		{domain.SceneInterview, map[string]string{"when": "three_days", "format": "final", "preparation": "key_piece", "impression": "reliable"}},
		{domain.SceneWedding, map[string]string{"role": "guest", "timing": "dinner", "dress_code": "elegant", "impression": "memorable"}},
		{domain.SceneDate, map[string]string{"activity": "dinner", "timing": "evening", "preparation": "closet", "impression": "natural"}},
		{domain.SceneDaily, map[string]string{"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "energetic"}},
		{domain.SceneGathering, map[string]string{"activity": "friends", "timing": "night", "preparation": "complete", "impression": "memorable"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.scene), func(t *testing.T) {
			got, err := NormalizeBrief(tt.scene, tt.answers)
			if err != nil {
				t.Fatal(err)
			}
			if got.SchemaVersion != BriefSchemaVersion || len(got.Answers) != len(tt.answers) {
				t.Fatalf("unexpected brief: %#v", got)
			}
		})
	}
}

func TestNormalizeBriefRejectsUnknownMissingAndExtraAnswers(t *testing.T) {
	if _, err := NormalizeBrief(domain.Scene("party"), map[string]string{}); !errors.Is(err, ErrInvalidBrief) {
		t.Fatalf("unknown scene: got %v", err)
	}
	if _, err := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned",
	}); !errors.Is(err, ErrInvalidBrief) {
		t.Fatalf("missing answers: got %v", err)
	}
	_, err := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet",
		"impression": "natural", "budget": "unknown",
	})
	if !errors.Is(err, ErrInvalidBrief) {
		t.Fatalf("extra answer: got %v, want ErrInvalidBrief", err)
	}
	if _, err := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "beach", "weather": "air_conditioned", "preparation": "closet",
		"impression": "natural",
	}); !errors.Is(err, ErrInvalidBrief) {
		t.Fatalf("unlisted value: got %v", err)
	}
}

func TestBriefHashIsMapOrderIndependent(t *testing.T) {
	a, _ := NormalizeBrief(domain.SceneGeneral, map[string]string{"focus": "balanced", "preparation": "closet", "impression": "natural"})
	b, _ := NormalizeBrief(domain.SceneGeneral, map[string]string{"impression": "natural", "focus": "balanced", "preparation": "closet"})
	if BriefHash(a) != BriefHash(b) {
		t.Fatal("semantically identical briefs must hash equally")
	}
	c, _ := NormalizeBrief(domain.SceneGeneral, map[string]string{"focus": "hair_first", "preparation": "closet", "impression": "natural"})
	if BriefHash(a) == BriefHash(c) {
		t.Fatal("different briefs must hash differently")
	}
	if len(BriefHash(a)) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(BriefHash(a)))
	}
}

func TestStyleRuleCatalogIsFrozen(t *testing.T) {
	if len(StyleRuleIDs) != 5 || !IsStyleRuleID(StyleRuleSceneFormality) || IsStyleRuleID("style.provider_invented") {
		t.Fatalf("style rule catalog drifted: %#v", StyleRuleIDs)
	}
}
