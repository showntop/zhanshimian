package domain

import (
	"encoding/json"
	"time"
)

type TaskType string
type TaskStatus string
type ErrorClass string
type TaskDisposition string
type CommitOutcome string

const (
	TaskQueued     TaskStatus = "queued"
	TaskLeased     TaskStatus = "leased"
	TaskRetryWait  TaskStatus = "retry_wait"
	TaskSucceeded  TaskStatus = "succeeded"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
	TaskSuperseded TaskStatus = "superseded"

	ErrorTransient       ErrorClass = "transient"
	ErrorThrottled       ErrorClass = "throttled"
	ErrorPermanent       ErrorClass = "permanent"
	ErrorQualityRejected ErrorClass = "quality_rejected"
	ErrorSuperseded      ErrorClass = "superseded"

	TaskPublish     TaskDisposition = "publish"
	TaskEnqueueNext TaskDisposition = "enqueue_next"
	TaskDomainFail  TaskDisposition = "domain_failed"

	CommitApplied    CommitOutcome = "applied"
	CommitSuperseded CommitOutcome = "superseded"
)

func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskSucceeded, TaskFailed, TaskCancelled, TaskSuperseded:
		return true
	default:
		return false
	}
}

func (c ErrorClass) Retryable() bool {
	switch c {
	case ErrorTransient, ErrorThrottled:
		return true
	default:
		return false
	}
}

type Task struct {
	ID                string
	UserID            string
	OperationID       string
	Type              TaskType
	SubjectType       string
	SubjectID         string
	SubjectGeneration int64
	PayloadVersion    int
	Payload           json.RawMessage
	DedupeKey         string
	Status            TaskStatus
	Priority          int
	Attempt           int
	MaxAttempts       int
	AvailableAt       time.Time
	HeartbeatAt       *time.Time
	CancelRequestedAt *time.Time
	ProgressBPS       int
	StageCode         string
	ErrorClass        ErrorClass
	ErrorCode         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	FinishedAt        *time.Time
}

type TaskLease struct {
	Task
	LeaseToken     string
	LeaseOwner     string
	LeaseExpiresAt time.Time
}

type TaskFailure struct {
	Class ErrorClass
	Code  string
}

type TaskResult struct {
	Disposition TaskDisposition
	ResultType  string
	ResultID    string
	Failure     *TaskFailure
}
