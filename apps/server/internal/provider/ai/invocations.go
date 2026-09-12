package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

const finishWriteTimeout = 3 * time.Second

var requestHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type InvocationStore interface {
	StartInvocation(context.Context, domain.StartInvocation) (domain.ProviderInvocation, error)
	FinishInvocation(context.Context, domain.FinishInvocation) error
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type InvocationRecorder struct {
	store InvocationStore
	clock Clock
}

func NewInvocationRecorder(store InvocationStore, clock Clock) *InvocationRecorder {
	if clock == nil {
		clock = realClock{}
	}
	return &InvocationRecorder{store: store, clock: clock}
}

type CallResult struct {
	ProviderRequestID string
	InputTokens       *int
	OutputTokens      *int
	InputImages       *int
	OutputImages      *int
	EstimatedCostCNY  *float64
}

type InvocationMeta struct {
	InvocationID      string
	ProviderRequestID string
	LatencyMS         int
	EstimatedCostCNY  *float64
}

type CallError struct {
	Class domain.ErrorClass
	Code  string
}

func NewCallError(class domain.ErrorClass, code string) *CallError {
	return &CallError{Class: class, Code: code}
}

func (e *CallError) Error() string {
	if e == nil {
		return "call error"
	}
	if e.Code == "" {
		return string(e.Class)
	}
	return string(e.Class) + "/" + e.Code
}

func (r *InvocationRecorder) Record(ctx context.Context, in domain.StartInvocation, call func(context.Context) (CallResult, error)) (CallResult, InvocationMeta, error) {
	return r.RecordInvocation(ctx, in, call)
}

func (r *InvocationRecorder) RecordInvocation(ctx context.Context, in domain.StartInvocation, call func(context.Context) (CallResult, error)) (CallResult, InvocationMeta, error) {
	in.RequestHash = normalizeRequestHash(in.RequestHash)
	started, err := r.store.StartInvocation(ctx, in)
	if err != nil {
		return CallResult{}, InvocationMeta{}, err
	}
	began := r.clock.Now()
	result, callErr := call(ctx)
	latencyMS := int(r.clock.Now().Sub(began) / time.Millisecond)
	meta := InvocationMeta{
		InvocationID:      started.ID,
		ProviderRequestID: result.ProviderRequestID,
		LatencyMS:         latencyMS,
		EstimatedCostCNY:  result.EstimatedCostCNY,
	}
	finish := domain.FinishInvocation{
		UserID:            started.UserID,
		InvocationID:      started.ID,
		TaskID:            started.TaskID,
		AttemptNo:         started.AttemptNo,
		ProviderRequestID: result.ProviderRequestID,
		InputTokens:       result.InputTokens,
		OutputTokens:      result.OutputTokens,
		InputImages:       result.InputImages,
		OutputImages:      result.OutputImages,
		EstimatedCostCNY:  result.EstimatedCostCNY,
		LatencyMS:         &latencyMS,
	}
	if callErr != nil {
		finish.Status = domain.InvocationFailed
		finish.ErrorClass, finish.ErrorCode = classifyCall(callErr)
	} else {
		finish.Status = domain.InvocationSucceeded
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishWriteTimeout)
	defer cancel()
	if err := r.store.FinishInvocation(writeCtx, finish); err != nil && callErr == nil {
		return result, meta, err
	}
	return result, meta, callErr
}

func normalizeRequestHash(value string) string {
	if requestHashPattern.MatchString(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func classifyCall(err error) (domain.ErrorClass, string) {
	var callErr *CallError
	if errors.As(err, &callErr) && callErr != nil && callErr.Class != "" {
		code := callErr.Code
		if code == "" {
			code = "unclassified"
		}
		return callErr.Class, code
	}
	return domain.ErrorPermanent, "unclassified"
}
