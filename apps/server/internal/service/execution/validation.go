package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	minIdempotencyKeyLen = 8
	maxIdempotencyKeyLen = 128
)

// selectionFingerprint 是哈希前的固定规范化结构,只包含 trim 后的三个标识。
type selectionFingerprint struct {
	PlanSetID           string  `json:"plan_set_id"`
	PlanVariantID       string  `json:"plan_variant_id"`
	RenderPublicationID *string `json:"render_publication_id,omitempty"`
}

// executionFingerprint 是 Execution 请求哈希前的固定规范化结构。
type executionFingerprint struct {
	SelectionID string `json:"selection_id"`
}

// normalizeSelection 校验并规范化一次选择请求,产出 trim 后的标识与请求哈希。
func normalizeSelection(planSetID string, input PutSelectionInput) (string, string, *string, string, error) {
	planSetID = strings.TrimSpace(planSetID)
	variantID := strings.TrimSpace(input.PlanVariantID)
	var publicationID *string
	if input.RenderPublicationID != nil {
		v := strings.TrimSpace(*input.RenderPublicationID)
		publicationID = &v
	}
	if err := requireUUID("plan_set_id", planSetID); err != nil {
		return "", "", nil, "", err
	}
	if err := requireUUID("plan_variant_id", variantID); err != nil {
		return "", "", nil, "", err
	}
	if publicationID != nil {
		if err := requireUUID("render_publication_id", *publicationID); err != nil {
			return "", "", nil, "", err
		}
	}
	if err := requireIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", "", nil, "", err
	}
	raw, err := json.Marshal(selectionFingerprint{
		PlanSetID: planSetID, PlanVariantID: variantID, RenderPublicationID: publicationID,
	})
	if err != nil {
		return "", "", nil, "", err
	}
	sum := sha256.Sum256(raw)
	return planSetID, variantID, publicationID, hex.EncodeToString(sum[:]), nil
}

// normalizeExecution 校验并规范化一次快照请求,产出 trim 后的 selectionID 与请求哈希。
func normalizeExecution(selectionID string, input CreateExecutionInput) (string, string, error) {
	selectionID = strings.TrimSpace(selectionID)
	if err := requireUUID("selection_id", selectionID); err != nil {
		return "", "", err
	}
	if err := requireIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(executionFingerprint{SelectionID: selectionID})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return selectionID, hex.EncodeToString(sum[:]), nil
}

func requireUUID(field, value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("execution: %s must be a valid UUID: %w", field, err)
	}
	return nil
}

func requireIdempotencyKey(key string) error {
	if len(key) < minIdempotencyKeyLen || len(key) > maxIdempotencyKeyLen {
		return fmt.Errorf("execution: idempotency key must be %d-%d bytes, got %d",
			minIdempotencyKeyLen, maxIdempotencyKeyLen, len(key))
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return fmt.Errorf("execution: idempotency key must be printable ASCII")
		}
	}
	return nil
}
