package domain

import (
	"errors"
	"testing"
)

func TestValidateTransitionRequiresAllStepsBeforeCompleted(t *testing.T) {
	got, err := ValidateTransition(ExecutionActive, EventCompleted, false)
	if !errors.Is(err, ErrInvalidTransition) || got != ExecutionActive {
		t.Fatalf("got state=%q err=%v", got, err)
	}
}

// TestValidateTransitionTable 锁定唯一状态表,防止后续任务改坏投影。
func TestValidateTransitionTable(t *testing.T) {
	cases := []struct {
		name              string
		state             ExecutionState
		event             ExecutionEventType
		allStepsCompleted bool
		wantState         ExecutionState
		wantErr           error
	}{
		{"planned+started", ExecutionPlanned, EventStarted, false, ExecutionActive, nil},
		{"planned+step_completed", ExecutionPlanned, EventStepCompleted, false, ExecutionActive, nil},
		{"planned+step_reopened", ExecutionPlanned, EventStepReopened, false, ExecutionActive, nil},
		{"planned+abandoned", ExecutionPlanned, EventAbandoned, false, ExecutionAbandoned, nil},
		{"planned+completed", ExecutionPlanned, EventCompleted, true, ExecutionPlanned, ErrInvalidTransition},
		{"active+step_completed", ExecutionActive, EventStepCompleted, false, ExecutionActive, nil},
		{"active+step_reopened", ExecutionActive, EventStepReopened, false, ExecutionActive, nil},
		{"active+completed_all", ExecutionActive, EventCompleted, true, ExecutionCompleted, nil},
		{"active+completed_partial", ExecutionActive, EventCompleted, false, ExecutionActive, ErrInvalidTransition},
		{"active+started", ExecutionActive, EventStarted, false, ExecutionActive, ErrInvalidTransition},
		{"active+abandoned", ExecutionActive, EventAbandoned, false, ExecutionAbandoned, nil},
		{"completed+step_completed", ExecutionCompleted, EventStepCompleted, false, ExecutionCompleted, ErrInvalidTransition},
		{"abandoned+started", ExecutionAbandoned, EventStarted, false, ExecutionAbandoned, ErrInvalidTransition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateTransition(tc.state, tc.event, tc.allStepsCompleted)
			if got != tc.wantState {
				t.Fatalf("state = %q, want %q", got, tc.wantState)
			}
			if (err == nil) != (tc.wantErr == nil) || (err != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
