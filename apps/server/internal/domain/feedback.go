package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Tag 是两类反馈的固定标签;Generation 标签绝不形成偏好记忆。
type Tag string

const (
	GenerationIdentityMismatch Tag = "identity_mismatch"
	GenerationHairMismatch     Tag = "hair_mismatch"
	GenerationMakeupMismatch   Tag = "makeup_mismatch"
	GenerationOutfitMismatch   Tag = "outfit_mismatch"
	GenerationAnatomyIssue     Tag = "anatomy_issue"
	GenerationUnnatural        Tag = "unnatural"
	ExecutionEasy              Tag = "easy_to_execute"
	ExecutionTooFormal         Tag = "too_formal"
	ExecutionTooComplex        Tag = "too_complex"
	ExecutionDislikeColor      Tag = "dislike_color"
	ExecutionWantToKeep        Tag = "want_to_keep"
)

// PreferenceKind 是结构化偏好的动作。
type PreferenceKind string

const (
	PreferenceLessFormal PreferenceKind = "less_formal"
	PreferenceSimplify   PreferenceKind = "simplify"
	PreferenceAvoid      PreferenceKind = "avoid"
	PreferencePreserve   PreferenceKind = "preserve"
)

// PreferenceCategory 是偏好作用的维度。
type PreferenceCategory string

const (
	CategoryOverall PreferenceCategory = "overall"
	CategoryHair    PreferenceCategory = "hair"
	CategoryMakeup  PreferenceCategory = "makeup"
	CategoryOutfit  PreferenceCategory = "outfit"
	CategoryColor   PreferenceCategory = "color"
)

// StructuredPreference 是客户端明确表达的偏好;只有它能形成记忆。
type StructuredPreference struct {
	Kind     PreferenceKind     `json:"kind,omitempty"`
	Category PreferenceCategory `json:"category,omitempty"`
	Value    string             `json:"value,omitempty"`
}

// PreferenceMemoryDraft 是待持久化的偏好记忆。
type PreferenceMemoryDraft struct {
	Key       string             `json:"key"`
	Category  PreferenceCategory `json:"category"`
	Value     string             `json:"value"`
	SourceTag Tag                `json:"source_tag"`
}

// AcknowledgementCode 是反馈响应的准确确认码;页面只承诺已写库的记忆。
type AcknowledgementCode string

const (
	AckFeedbackRecorded AcknowledgementCode = "feedback_recorded"
	AckLessFormalSaved  AcknowledgementCode = "less_formal_saved"
	AckSimplerSaved     AcknowledgementCode = "simpler_saved"
	AckAvoidColorSaved  AcknowledgementCode = "avoid_color_saved"
	AckPreserveSaved    AcknowledgementCode = "preserve_saved"
)

// ErrPreferenceNotAllowed 表示 Generation 反馈不得形成偏好记忆。
var ErrPreferenceNotAllowed = errors.New("preference memory not allowed for this feedback")

var generationTags = map[Tag]bool{
	GenerationIdentityMismatch: true,
	GenerationHairMismatch:     true,
	GenerationMakeupMismatch:   true,
	GenerationOutfitMismatch:   true,
	GenerationAnatomyIssue:     true,
	GenerationUnnatural:        true,
}

// NormalizePreference 只从结构化输入确定性地产生 0 或 1 条偏好记忆;Generation
// 标签一律返回 ErrPreferenceNotAllowed,自由文本永不进入。
func NormalizePreference(input StructuredPreference, tags []Tag) ([]PreferenceMemoryDraft, error) {
	for _, t := range tags {
		if generationTags[t] {
			return nil, ErrPreferenceNotAllowed
		}
	}
	hasTag := func(want Tag) bool {
		for _, t := range tags {
			if t == want {
				return true
			}
		}
		return false
	}
	trimmed := func() (string, bool) {
		v := strings.TrimSpace(input.Value)
		return v, v != "" && utf8.RuneCountInString(v) <= 40
	}
	switch input.Kind {
	case PreferenceLessFormal:
		if input.Category == CategoryOverall && hasTag(ExecutionTooFormal) {
			return []PreferenceMemoryDraft{{Key: "formality", Category: CategoryOverall, Value: "less", SourceTag: ExecutionTooFormal}}, nil
		}
	case PreferenceSimplify:
		if input.Category == CategoryOverall && hasTag(ExecutionTooComplex) {
			return []PreferenceMemoryDraft{{Key: "complexity", Category: CategoryOverall, Value: "simpler", SourceTag: ExecutionTooComplex}}, nil
		}
	case PreferenceAvoid:
		if input.Category == CategoryColor && hasTag(ExecutionDislikeColor) {
			if v, ok := trimmed(); ok {
				return []PreferenceMemoryDraft{{Key: "avoid_color", Category: CategoryColor, Value: v, SourceTag: ExecutionDislikeColor}}, nil
			}
		}
	case PreferencePreserve:
		switch input.Category {
		case CategoryHair, CategoryMakeup, CategoryOutfit:
			if hasTag(ExecutionWantToKeep) {
				if v, ok := trimmed(); ok {
					return []PreferenceMemoryDraft{{Key: "preserve_" + string(input.Category), Category: input.Category, Value: v, SourceTag: ExecutionWantToKeep}}, nil
				}
			}
		}
	}
	return nil, nil
}

// AcknowledgementCodeFor 根据已持久化记忆返回精确确认码;无记忆只确认记录完成。
func AcknowledgementCodeFor(memories []PreferenceMemoryDraft) AcknowledgementCode {
	if len(memories) == 0 {
		return AckFeedbackRecorded
	}
	switch memories[0].Key {
	case "formality":
		return AckLessFormalSaved
	case "complexity":
		return AckSimplerSaved
	case "avoid_color":
		return AckAvoidColorSaved
	case "preserve_hair", "preserve_makeup", "preserve_outfit":
		return AckPreserveSaved
	default:
		return AckFeedbackRecorded
	}
}
