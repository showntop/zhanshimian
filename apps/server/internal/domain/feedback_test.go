package domain

import (
	"errors"
	"testing"
)

func TestNormalizePreferenceOnlyUsesStructuredInput(t *testing.T) {
	got, err := NormalizePreference(
		StructuredPreference{Kind: PreferenceLessFormal, Category: CategoryOverall},
		[]Tag{ExecutionTooFormal},
	)
	if err != nil || len(got) != 1 || got[0].Key != "formality" || got[0].Value != "less" {
		t.Fatalf("unexpected memory: %#v err=%v", got, err)
	}
	none, err := NormalizePreference(StructuredPreference{}, []Tag{ExecutionEasy})
	if err != nil || len(none) != 0 {
		t.Fatalf("non-explicit feedback must not become memory: %#v err=%v", none, err)
	}
}

func TestGenerationTagsNeverCreatePreferenceMemory(t *testing.T) {
	_, err := NormalizePreference(
		StructuredPreference{Kind: PreferencePreserve, Category: CategoryHair, Value: "当前发型"},
		[]Tag{Tag("identity_mismatch")},
	)
	if !errors.Is(err, ErrPreferenceNotAllowed) {
		t.Fatalf("generation feedback must not create memory: %v", err)
	}
}

// TestNormalizePreferenceMapping 锁定唯一的标签→记忆映射。
func TestNormalizePreferenceMapping(t *testing.T) {
	cases := []struct {
		name     string
		input    StructuredPreference
		tags     []Tag
		wantKey  string
		wantCat  PreferenceCategory
		wantVal  string
		wantErr  error
		wantNone bool
	}{
		{
			name:  "too_formal→formality",
			input: StructuredPreference{Kind: PreferenceLessFormal, Category: CategoryOverall},
			tags:  []Tag{ExecutionTooFormal},
			wantKey: "formality", wantCat: CategoryOverall, wantVal: "less",
		},
		{
			name:  "too_complex→complexity",
			input: StructuredPreference{Kind: PreferenceSimplify, Category: CategoryOverall},
			tags:  []Tag{ExecutionTooComplex},
			wantKey: "complexity", wantCat: CategoryOverall, wantVal: "simpler",
		},
		{
			name:  "dislike_color→avoid_color",
			input: StructuredPreference{Kind: PreferenceAvoid, Category: CategoryColor, Value: " 荧光绿 "},
			tags:  []Tag{ExecutionDislikeColor},
			wantKey: "avoid_color", wantCat: CategoryColor, wantVal: "荧光绿",
		},
		{
			name:  "want_to_keep→preserve_outfit",
			input: StructuredPreference{Kind: PreferencePreserve, Category: CategoryOutfit, Value: "自然偏分"},
			tags:  []Tag{ExecutionWantToKeep},
			wantKey: "preserve_outfit", wantCat: CategoryOutfit, wantVal: "自然偏分",
		},
		{name: "easy_to_execute无记忆", tags: []Tag{ExecutionEasy}, wantNone: true},
		{name: "无结构偏好无记忆", input: StructuredPreference{}, tags: []Tag{ExecutionEasy}, wantNone: true},
		{name: "kind与tag不匹配无记忆", input: StructuredPreference{Kind: PreferenceLessFormal, Category: CategoryOverall}, tags: []Tag{ExecutionTooComplex}, wantNone: true},
		{name: "avoid空值无记忆", input: StructuredPreference{Kind: PreferenceAvoid, Category: CategoryColor, Value: "  "}, tags: []Tag{ExecutionDislikeColor}, wantNone: true},
		{
			name:    "generation标签报错",
			input:   StructuredPreference{Kind: PreferencePreserve, Category: CategoryHair, Value: "x"},
			tags:    []Tag{GenerationHairMismatch},
			wantErr: ErrPreferenceNotAllowed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizePreference(tc.input, tc.tags)
			if (err == nil) != (tc.wantErr == nil) || (err != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("expected no memory, got %#v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d memories, want 1: %#v", len(got), got)
			}
			m := got[0]
			if m.Key != tc.wantKey || m.Category != tc.wantCat || m.Value != tc.wantVal {
				t.Fatalf("memory = %#v, want key=%q cat=%q val=%q", m, tc.wantKey, tc.wantCat, tc.wantVal)
			}
		})
	}
}

func TestAcknowledgementCodeFor(t *testing.T) {
	cases := []struct {
		memories []PreferenceMemoryDraft
		want     AcknowledgementCode
	}{
		{nil, AckFeedbackRecorded},
		{[]PreferenceMemoryDraft{}, AckFeedbackRecorded},
		{[]PreferenceMemoryDraft{{Key: "formality"}}, AckLessFormalSaved},
		{[]PreferenceMemoryDraft{{Key: "complexity"}}, AckSimplerSaved},
		{[]PreferenceMemoryDraft{{Key: "avoid_color"}}, AckAvoidColorSaved},
		{[]PreferenceMemoryDraft{{Key: "preserve_hair"}}, AckPreserveSaved},
		{[]PreferenceMemoryDraft{{Key: "preserve_outfit"}}, AckPreserveSaved},
	}
	for _, tc := range cases {
		if got := AcknowledgementCodeFor(tc.memories); got != tc.want {
			t.Fatalf("AcknowledgementCodeFor(%#v) = %q, want %q", tc.memories, got, tc.want)
		}
	}
}
