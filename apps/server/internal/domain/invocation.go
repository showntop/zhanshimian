package domain

import (
	"context"
	"time"
)

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
	ReleaseBucket        *int
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
	ReleaseBucket        *int
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

// InvocationScope 是把一次 AI 调用归集到 (user, operation, task, attempt)
// 的任务身份。taskrunner 在 Execute 前注入 ctx；provider 运行时读取它决定
// 是否写 provider_invocations 台账（无 scope 的同步 API 调用不写）。
type InvocationScope struct {
	UserID      string
	OperationID string
	TaskID      string
	AttemptNo   int
}

type invocationScopeKey struct{}

func WithInvocationScope(ctx context.Context, scope InvocationScope) context.Context {
	return context.WithValue(ctx, invocationScopeKey{}, scope)
}

func InvocationScopeFrom(ctx context.Context) (InvocationScope, bool) {
	scope, ok := ctx.Value(invocationScopeKey{}).(InvocationScope)
	return scope, ok
}
