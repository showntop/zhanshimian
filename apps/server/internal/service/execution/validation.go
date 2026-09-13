package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
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

// eventFingerprint 是事件请求哈希前的固定规范化结构。
type eventFingerprint struct {
	ExecutionID   string    `json:"execution_id"`
	ClientEventID string    `json:"client_event_id"`
	Type          string    `json:"type"`
	StepID        *string   `json:"step_id,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
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

// normalizeEvent 校验并规范化一次事件追加请求,产出 trim 后的标识、规范化
// 时间与请求哈希。step 事件必须携带 step_id,其它事件不得携带。
func normalizeEvent(executionID string, input AppendEventInput) (string, *string, time.Time, string, error) {
	executionID = strings.TrimSpace(executionID)
	if err := requireUUID("execution_id", executionID); err != nil {
		return "", nil, time.Time{}, "", err
	}
	clientEventID := strings.TrimSpace(input.ClientEventID)
	if err := requireClientEventID(clientEventID); err != nil {
		return "", nil, time.Time{}, "", err
	}
	var stepID *string
	if input.StepID != nil {
		v := strings.TrimSpace(*input.StepID)
		stepID = &v
	}
	switch input.Type {
	case domain.EventStepCompleted, domain.EventStepReopened:
		if stepID == nil {
			return "", nil, time.Time{}, "", fmt.Errorf("execution: step event requires step_id")
		}
		if err := requireUUID("step_id", *stepID); err != nil {
			return "", nil, time.Time{}, "", err
		}
	default:
		if stepID != nil {
			return "", nil, time.Time{}, "", fmt.Errorf("execution: %s event must not carry step_id", input.Type)
		}
	}
	if input.OccurredAt.IsZero() {
		return "", nil, time.Time{}, "", fmt.Errorf("execution: occurred_at is required")
	}
	occurredAt := input.OccurredAt.UTC()
	now := time.Now().UTC()
	if d := occurredAt.Sub(now); d > 24*time.Hour || d < -24*time.Hour {
		return "", nil, time.Time{}, "", fmt.Errorf("execution: occurred_at must be within 24h of server time")
	}
	if input.ExpectedVersion < 1 {
		return "", nil, time.Time{}, "", fmt.Errorf("execution: expected_version must be positive")
	}
	raw, err := json.Marshal(eventFingerprint{
		ExecutionID:   executionID,
		ClientEventID: clientEventID,
		Type:          string(input.Type),
		StepID:        stepID,
		OccurredAt:    occurredAt,
	})
	if err != nil {
		return "", nil, time.Time{}, "", err
	}
	sum := sha256.Sum256(raw)
	return executionID, stepID, occurredAt, hex.EncodeToString(sum[:]), nil
}

const maxClientEventIDLen = 200

func requireClientEventID(id string) error {
	if len(id) < 1 || len(id) > maxClientEventIDLen {
		return fmt.Errorf("execution: client_event_id must be 1-%d bytes, got %d", maxClientEventIDLen, len(id))
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x20 || id[i] > 0x7e {
			return fmt.Errorf("execution: client_event_id must be printable ASCII")
		}
	}
	return nil
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
