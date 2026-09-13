package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// ExecutionState 是 Execution 投影的状态;只有 planned/active 接受新事件。
type ExecutionState string

const (
	ExecutionPlanned   ExecutionState = "planned"
	ExecutionActive    ExecutionState = "active"
	ExecutionCompleted ExecutionState = "completed"
	ExecutionAbandoned ExecutionState = "abandoned"
)

// ExecutionEventType 是追加式事件的闭集。
type ExecutionEventType string

const (
	EventStarted       ExecutionEventType = "started"
	EventStepCompleted ExecutionEventType = "step_completed"
	EventStepReopened  ExecutionEventType = "step_reopened"
	EventCompleted     ExecutionEventType = "completed"
	EventAbandoned     ExecutionEventType = "abandoned"
)

// ErrInvalidTransition 表示该事件不允许在当前状态发生。
var ErrInvalidTransition = errors.New("invalid execution transition")

// ErrIdempotencyConflict 表示同一 Idempotency-Key 下的请求体发生了变化。
// 它驻留在 domain 以便 postgres 适配器在不依赖 service 层的前提下返回同一哨兵。
var ErrIdempotencyConflict = errors.New("idempotency conflict")

// PlanSelection 是独立且追加式的用户选择事实。
type PlanSelection struct {
	ID                  string    `json:"id"`
	PlanSetID           string    `json:"plan_set_id"`
	PlanVariantID       string    `json:"plan_variant_id"`
	RenderPublicationID *string   `json:"render_publication_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// CreateSelectionCommand 是跨边界 DTO:postgres 适配器接收它而不依赖 service 层。
type CreateSelectionCommand struct {
	UserID              string
	PlanSetID           string
	PlanVariantID       string
	RenderPublicationID *string
	IdempotencyKey      string
	RequestHash         string
}

// ExecutionStep 是创建 Execution 时从 PlanVariant 复制的不可变快照。
type ExecutionStep struct {
	ID               string          `json:"id"`
	SourcePlanStepID string          `json:"source_plan_step_id"`
	Category         string          `json:"category"`
	Action           string          `json:"action"`
	Title            string          `json:"title"`
	Summary          string          `json:"summary"`
	Details          json.RawMessage `json:"details"`
	Position         int             `json:"position"`
	Completed        bool            `json:"completed"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
}

// Execution 是可变的执行投影,只允许 CAS 更新 state/version/timestamps。
type Execution struct {
	ID          string          `json:"id"`
	SelectionID string          `json:"selection_id"`
	State       ExecutionState  `json:"state"`
	Version     int             `json:"version"`
	Steps       []ExecutionStep `json:"steps"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ExecutionEvent 是幂等追加事件,client_event_id 用于去重。
type ExecutionEvent struct {
	ID            string             `json:"id"`
	ExecutionID   string             `json:"execution_id"`
	ClientEventID string             `json:"client_event_id"`
	Type          ExecutionEventType `json:"type"`
	StepID        *string            `json:"step_id,omitempty"`
	OccurredAt    time.Time          `json:"occurred_at"`
	CreatedAt     time.Time          `json:"created_at"`
}

// ValidateTransition 按唯一状态表推进 Execution 投影;返回值始终是当前应保持的状态。
func ValidateTransition(state ExecutionState, event ExecutionEventType, allStepsCompleted bool) (ExecutionState, error) {
	switch state {
	case ExecutionPlanned:
		switch event {
		case EventStarted, EventStepCompleted, EventStepReopened:
			return ExecutionActive, nil
		case EventAbandoned:
			return ExecutionAbandoned, nil
		default:
			return state, ErrInvalidTransition
		}
	case ExecutionActive:
		switch event {
		case EventStepCompleted, EventStepReopened:
			return ExecutionActive, nil
		case EventCompleted:
			if allStepsCompleted {
				return ExecutionCompleted, nil
			}
			return state, ErrInvalidTransition
		case EventAbandoned:
			return ExecutionAbandoned, nil
		default:
			return state, ErrInvalidTransition
		}
	default:
		return state, ErrInvalidTransition
	}
}
