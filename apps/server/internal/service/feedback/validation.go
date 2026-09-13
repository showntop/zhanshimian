package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	minIdempotencyKeyLen = 8
	maxIdempotencyKeyLen = 128
	maxCommentRunes      = 500
	maxGenerationTags    = 6
	maxExecutionTags     = 5
)

// generationFeedbackFingerprint 是 Generation 反馈请求哈希前的固定规范化结构,
// 只包含 trim 后的标识、去重后的标签、trim 后的评论与可选反馈图片。
type generationFeedbackFingerprint struct {
	PublicationID string       `json:"publication_id"`
	Tags          []domain.Tag `json:"tags"`
	Comment       string       `json:"comment"`
	MediaAssetID  *string      `json:"media_asset_id,omitempty"`
}

// normalizeGenerationFeedback 校验并规范化一条 Generation 反馈请求,产出 trim
// 后的标识、去重后的标签、trim 后的评论、规范化图片 id 与请求哈希。
func normalizeGenerationFeedback(input CreateGenerationFeedbackInput) (string, []domain.Tag, string, *string, string, error) {
	publicationID := strings.TrimSpace(input.PublicationID)
	if err := requireUUID("publication_id", publicationID); err != nil {
		return "", nil, "", nil, "", err
	}
	tags, err := normalizeGenerationTags(input.Tags)
	if err != nil {
		return "", nil, "", nil, "", err
	}
	comment, err := normalizeComment(input.Comment)
	if err != nil {
		return "", nil, "", nil, "", err
	}
	mediaAssetID, err := normalizeMediaAssetID(input.MediaAssetID)
	if err != nil {
		return "", nil, "", nil, "", err
	}
	if err := requireIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", nil, "", nil, "", err
	}
	raw, err := json.Marshal(generationFeedbackFingerprint{
		PublicationID: publicationID, Tags: tags, Comment: comment, MediaAssetID: mediaAssetID,
	})
	if err != nil {
		return "", nil, "", nil, "", err
	}
	sum := sha256.Sum256(raw)
	return publicationID, tags, comment, mediaAssetID, hex.EncodeToString(sum[:]), nil
}

// normalizeGenerationTags 去重后保持客户端顺序,只允许六个 Generation 标签,
// 数量限定在 1-6。
func normalizeGenerationTags(tags []domain.Tag) ([]domain.Tag, error) {
	seen := make(map[domain.Tag]bool, len(tags))
	out := make([]domain.Tag, 0, len(tags))
	for _, t := range tags {
		t = domain.Tag(strings.TrimSpace(string(t)))
		if t == "" {
			continue
		}
		if !domain.IsGenerationTag(t) {
			return nil, fmt.Errorf("feedback: tag %q is not a generation tag", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) < 1 || len(out) > maxGenerationTags {
		return nil, fmt.Errorf("feedback: generation feedback requires 1-%d tags, got %d", maxGenerationTags, len(out))
	}
	return out, nil
}

// executionFeedbackFingerprint 是 Execution 反馈请求哈希前的固定规范化结构,
// 只包含 trim 后的执行标识、去重后的标签、trim 后的评论、可选反馈图片与 trim
// 后的结构化偏好。
type executionFeedbackFingerprint struct {
	ExecutionID  string                      `json:"execution_id"`
	Tags         []domain.Tag                `json:"tags"`
	Comment      string                      `json:"comment"`
	MediaAssetID *string                     `json:"media_asset_id,omitempty"`
	Preference   domain.StructuredPreference `json:"preference,omitempty"`
}

// normalizeExecutionFeedback 校验并规范化一条 Execution 反馈请求,产出 trim 后的
// 执行标识、去重后的标签、trim 后的评论、规范化图片 id、trim 后的偏好与请求哈希。
func normalizeExecutionFeedback(input CreateExecutionFeedbackInput) (string, []domain.Tag, string, *string, domain.StructuredPreference, string, error) {
	executionID := strings.TrimSpace(input.ExecutionID)
	if err := requireUUID("execution_id", executionID); err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	tags, err := normalizeExecutionTags(input.Tags)
	if err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	comment, err := normalizeComment(input.Comment)
	if err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	mediaAssetID, err := normalizeMediaAssetID(input.MediaAssetID)
	if err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	if err := requireIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	preference := normalizePreference(input.Preference)
	raw, err := json.Marshal(executionFeedbackFingerprint{
		ExecutionID: executionID, Tags: tags, Comment: comment, MediaAssetID: mediaAssetID, Preference: preference,
	})
	if err != nil {
		return "", nil, "", nil, domain.StructuredPreference{}, "", err
	}
	sum := sha256.Sum256(raw)
	return executionID, tags, comment, mediaAssetID, preference, hex.EncodeToString(sum[:]), nil
}

// normalizeExecutionTags 去重后保持客户端顺序,只允许五个 Execution 标签,
// 数量限定在 1-5。
func normalizeExecutionTags(tags []domain.Tag) ([]domain.Tag, error) {
	seen := make(map[domain.Tag]bool, len(tags))
	out := make([]domain.Tag, 0, len(tags))
	for _, t := range tags {
		t = domain.Tag(strings.TrimSpace(string(t)))
		if t == "" {
			continue
		}
		if !domain.IsExecutionTag(t) {
			return nil, fmt.Errorf("feedback: tag %q is not an execution tag", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) < 1 || len(out) > maxExecutionTags {
		return nil, fmt.Errorf("feedback: execution feedback requires 1-%d tags, got %d", maxExecutionTags, len(out))
	}
	return out, nil
}

// normalizePreference trim 结构化偏好的 Value,使同一逻辑偏好产出同一请求哈希。
func normalizePreference(preference domain.StructuredPreference) domain.StructuredPreference {
	preference.Value = strings.TrimSpace(preference.Value)
	return preference
}

// normalizeComment trim 后最多 500 rune。
func normalizeComment(comment string) (string, error) {
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > maxCommentRunes {
		return "", fmt.Errorf("feedback: comment must be at most %d runes", maxCommentRunes)
	}
	return comment, nil
}

// normalizeMediaAssetID 规范化可选的反馈图片 id。
func normalizeMediaAssetID(id *string) (*string, error) {
	if id == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*id)
	if err := requireUUID("media_asset_id", v); err != nil {
		return nil, err
	}
	return &v, nil
}

func requireUUID(field, value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("feedback: %s must be a valid UUID: %w", field, err)
	}
	return nil
}

func requireIdempotencyKey(key string) error {
	if len(key) < minIdempotencyKeyLen || len(key) > maxIdempotencyKeyLen {
		return fmt.Errorf("feedback: idempotency key must be %d-%d bytes, got %d",
			minIdempotencyKeyLen, maxIdempotencyKeyLen, len(key))
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return fmt.Errorf("feedback: idempotency key must be printable ASCII")
		}
	}
	return nil
}
