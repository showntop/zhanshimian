package domain

import "time"

type OperationKind string
type OperationStatus string

const (
	OperationAssessment        OperationKind = "assessment"
	OperationPlanSet           OperationKind = "plan_set"
	OperationRender            OperationKind = "render"
	OperationExecutionFeedback OperationKind = "execution_feedback"

	OperationAccepted   OperationStatus = "accepted"
	OperationRunning    OperationStatus = "running"
	OperationRetrying   OperationStatus = "retrying"
	OperationSucceeded  OperationStatus = "succeeded"
	OperationFailed     OperationStatus = "failed"
	OperationCancelled  OperationStatus = "cancelled"
	OperationSuperseded OperationStatus = "superseded"
)

// OperationRef is the public operation reference exposed beside 202
// envelopes; clients poll it instead of reading internal tasks.
type OperationRef struct {
	ID     string          `json:"id"`
	Kind   OperationKind   `json:"kind"`
	Status OperationStatus `json:"status"`
}

type Operation struct {
	ID            string
	UserID        string
	Kind          OperationKind
	SubjectType   string
	SubjectID     string
	Status        OperationStatus
	ProgressBPS   int
	StageCode     string
	PublicMessage string
	ErrorCode     string
	TraceID       string
	Retryable     bool
	ResultType    string
	ResultID      string
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	FinishedAt    *time.Time
}
