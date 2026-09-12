package domain

import (
	"encoding/json"
	"time"
)

type QualityDecision string
type QualitySubjectType string

const (
	QualityDecisionPass   QualityDecision = "pass"
	QualityDecisionRetry  QualityDecision = "retry"
	QualityDecisionReject QualityDecision = "reject"
	QualityDecisionError  QualityDecision = "error"

	QualitySubjectReport          QualitySubjectType = "report"
	QualitySubjectPlanSet         QualitySubjectType = "plan_set"
	QualitySubjectRenderCandidate QualitySubjectType = "render_candidate"
)

type QualityPolicyRef struct {
	Version string
}

type QualityEvaluation struct {
	ID                    string
	UserID                string
	SubjectType           QualitySubjectType
	SubjectID             string
	Policy                QualityPolicyRef
	Decision              QualityDecision
	ReasonCodes           []string
	InternalScores        json.RawMessage
	EvaluatorInvocationID string
	CreatedAt             time.Time
}
