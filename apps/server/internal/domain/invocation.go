package domain

import "time"

type InvocationStatus string

const (
	InvocationStarted   InvocationStatus = "started"
	InvocationSucceeded InvocationStatus = "succeeded"
	InvocationFailed    InvocationStatus = "failed"
)

type ProviderInvocation struct {
	ID                   string
	UserID               string
	OperationID          string
	TaskID               string
	AttemptNo            int
	Capability           string
	RoutingConfigVersion string
	ProviderKey          string
	ModelKey             string
	Protocol             string
	RequestHash          string
	ProviderRequestID    string
	Status               InvocationStatus
	InputTokens          *int
	OutputTokens         *int
	InputImages          *int
	OutputImages         *int
	EstimatedCostCNY     *float64
	LatencyMS            *int
	ErrorClass           ErrorClass
	ErrorCode            string
	StartedAt            time.Time
	FinishedAt           *time.Time
	CreatedAt            time.Time
}

type StartInvocation struct {
	UserID               string
	OperationID          string
	TaskID               string
	AttemptNo            int
	Capability           string
	RoutingConfigVersion string
	ProviderKey          string
	ModelKey             string
	Protocol             string
	RequestHash          string
	InputImages          int
}

type FinishInvocation struct {
	UserID            string
	InvocationID      string
	TaskID            string
	AttemptNo         int
	Status            InvocationStatus
	ProviderRequestID string
	InputTokens       *int
	OutputTokens      *int
	InputImages       *int
	OutputImages      *int
	EstimatedCostCNY  *float64
	LatencyMS         *int
	ErrorClass        ErrorClass
	ErrorCode         string
}
